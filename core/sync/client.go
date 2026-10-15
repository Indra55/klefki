package sync

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

type Session struct {
	Token   string `json:"token"`
	Device  string `json:"device"`
	Expires int64  `json:"expires"`
}
type Remote struct {
	base     string
	client   *http.Client
	device   string
	deviceMu sync.RWMutex
}

func NewRemote(address string, client *http.Client, allowHTTP bool) (*Remote, error) {
	u, e := url.Parse(address)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, ErrInvalid
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !allowHTTP || ip == nil || !ip.IsLoopback() {
			return nil, ErrInvalid
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copyClient := *client
	// Never forward bearer credentials to a redirect destination.
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout == 0 {
		copyClient.Timeout = 30 * time.Second
	}
	return &Remote{base: strings.TrimSuffix(address, "/"), client: &copyClient}, nil
}

type HTTPError struct {
	Status     int
	retryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("sync HTTP status %d", e.Status) }
func (r *Remote) request(ctx context.Context, method, path, token string, body any, limit int64) ([]byte, error) {
	var data []byte
	var e error
	binary := false
	if body != nil {
		if b, ok := body.([]byte); ok {
			data = b
			binary = true
		} else {
			data, e = json.Marshal(body)
			defer vc.Wipe(data)
			if e != nil {
				return nil, ErrInvalid
			}
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, r.base+path, bytes.NewReader(data))
	if e != nil {
		return nil, ErrInvalid
	}
	req.Header.Set("X-Klefki-Deletion", "1")
	req.Header.Set("X-Klefki-Content", "1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		r.deviceMu.RLock()
		device := r.device
		r.deviceMu.RUnlock()
		if device != "" {
			req.Header.Set("X-Klefki-Device", device)
		}
	}
	if origin, ok := ctx.Value(originKey{}).(string); ok && origin != "" {
		req.Header.Set("X-Klefki-Origin-Device", origin)
	}
	if binary {
		req.Header.Set("Content-Type", "application/octet-stream")
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, e := r.client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if guard, ok := ctx.Value(contentGuardKey{}).(func() error); ok {
			if e := guard(); e != nil {
				return nil, e
			}
		}
		return nil, errors.Join(ErrRemote, e)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		after := time.Duration(0)
		if seconds, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil && seconds > 0 && seconds <= 120 {
			after = time.Duration(seconds) * time.Second
		}
		return nil, &HTTPError{Status: res.StatusCode, retryAfter: after}
	}
	var reader io.Reader = io.LimitReader(res.Body, limit+1)
	if observe, ok := ctx.Value(readProgressKey{}).(func(uint64)); ok {
		reader = &progressReader{reader: reader, report: observe}
	}
	b, e := io.ReadAll(reader)
	if e != nil {
		return nil, errors.Join(ErrRemote, e)
	}
	if int64(len(b)) > limit {
		return nil, ErrInvalid
	}
	return b, nil
}
func parseJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (r *Remote) Signup(ctx context.Context, a vc.Account, credential vc.Key) (Session, error) {
	defer vc.Wipe(credential[:])
	return r.session(ctx, "/v1/signup", struct {
		Account         string `json:"account"`
		Descriptor      []byte `json:"descriptor"`
		PasswordWrapper []byte `json:"password_wrapper"`
		RecoveryWrapper []byte `json:"recovery_wrapper"`
		Credential      []byte `json:"credential"`
	}{a.ID.String(), a.Descriptor, a.PasswordWrapper, a.RecoveryWrapper, credential[:]})
}
func (r *Remote) Login(ctx context.Context, account vc.ID, credential vc.Key) (Session, error) {
	defer vc.Wipe(credential[:])
	return r.session(ctx, "/v1/login", struct {
		Account    string `json:"account"`
		Credential []byte `json:"credential"`
	}{account.String(), credential[:]})
}
func (r *Remote) session(ctx context.Context, path string, in any) (Session, error) {
	var s Session
	b, e := r.request(ctx, "POST", path, "", in, 8192)
	if e != nil {
		return s, e
	}
	if e = parseJSON(b, &s); e != nil {
		return s, e
	}
	token, tokenErr := hex.DecodeString(s.Token)
	vc.Wipe(token)
	if tokenErr != nil || len(token) != 32 || s.Expires <= 0 {
		return Session{}, ErrInvalid
	}
	if _, e = parseID(s.Device); e != nil {
		return Session{}, e
	}
	return s, nil
}
func (r *Remote) Revoke(ctx context.Context, s Session) error {
	_, e := r.request(ctx, "DELETE", "/v1/devices/"+s.Device, s.Token, nil, 1024)
	return e
}

type Retry struct {
	Attempts  int
	Base, Max time.Duration
}
type Client struct {
	Store      *Store
	Remote     *Remote
	Session    Session
	Master     vc.Key
	Retry      Retry
	Observe    func(syncwire.Progress)
	OnContact  func()
	progress   syncwire.Progress
	progressMu sync.Mutex
	cooldownMu sync.Mutex
	cooldown   time.Time
}

func (c *Client) request(ctx context.Context, method, path string, body any, limit int64) ([]byte, error) {
	attempts := c.Retry.Attempts
	if attempts <= 0 {
		attempts = 4
	}
	if attempts > 10 {
		attempts = 10
	}
	delay := c.Retry.Base
	if delay <= 0 {
		delay = 250 * time.Millisecond
	}
	maxDelay := c.Retry.Max
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	maxDelay = min(maxDelay, 30*time.Second)
	delay = min(delay, maxDelay)
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if e := c.waitCooldown(ctx); e != nil {
			return nil, e
		}
		b, e := c.Remote.request(ctx, method, path, c.Session.Token, body, limit)
		if e == nil {
			if c.OnContact != nil {
				c.progressMu.Lock()
				c.OnContact()
				c.progressMu.Unlock()
			}
			return b, nil
		}
		retry := errors.Is(e, ErrRemote)
		wait := delay
		if h, ok := e.(*HTTPError); ok {
			retry = h.Status == 408 || h.Status == 429 || h.Status >= 500 && h.Status != 507
			wait = max(wait, h.retryAfter)
			if h.retryAfter > 0 {
				c.cooldownMu.Lock()
				until := time.Now().Add(h.retryAfter)
				if until.After(c.cooldown) {
					c.cooldown = until
				}
				c.cooldownMu.Unlock()
			}
		}
		if !retry || attempt+1 >= attempts {
			return nil, e
		}
		var previousPhase string
		c.updateProgress(func(p *syncwire.Progress) {
			previousPhase = p.Phase
			p.Phase, p.Attempt, p.RetryAt = "retrying", attempt+2, time.Now().Add(wait).UnixMilli()
		})
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		c.updateProgress(func(p *syncwire.Progress) { p.Phase, p.Attempt, p.RetryAt = previousPhase, 0, 0 })
		delay = min(delay*2, maxDelay)
	}
}
func (c *Client) Sync(ctx context.Context) error {
	c.report(syncwire.Progress{Phase: "checking"})
	if c.Store == nil || c.Remote == nil {
		return ErrInvalid
	}
	c.Store.syncMu.Lock()
	defer c.Store.syncMu.Unlock()

	err := c.syncLocked(ctx)
	var he *HTTPError
	if errors.As(err, &he) && he.Status == 410 {
		if e := c.syncDeletions(ctx); e != nil {
			return e
		}
		return c.syncLocked(ctx)
	}
	return err
}

func (c *Client) syncLocked(ctx context.Context) error {
	local, e := c.Store.RelayState()
	if e != nil {
		return e
	}
	c.Remote.deviceMu.Lock()
	c.Remote.device = local.Device
	c.Remote.deviceMu.Unlock()
	for {
		settings, err := c.Settings(ctx)
		if err != nil {
			return err
		}
		if !settings.SelectiveContent {
			return ErrContentUnsupported
		}
		err = c.syncSelective(ctx, settings)
		if !errors.Is(err, errContentReplan) {
			return err
		}
	}
}

func (c *Client) finishTrashSync(ctx context.Context) error {
	if e := c.syncDeletions(ctx); e != nil {
		return e
	}
	if c.Store.DeletionSupported() {
		if e := c.Store.InitializeTrash(c.Master, time.Now()); e != nil {
			return e
		}
		expired, e := c.Store.ExpiredTrash(c.Master, time.Now())
		if e != nil {
			return e
		}
		for _, p := range expired {
			heads, e := c.Store.Heads(c.Master)
			if e != nil {
				return e
			}
			targets := OrderDeletionTargets(heads, p.Targets)
			for start := 0; start < len(targets); start += 256 {
				batch := targets[start:min(start+256, len(targets))]
				if _, e = c.request(ctx, "POST", "/v1/sync/deletions", syncwire.DeletionBatch{Deletions: batch}, 1024); e != nil {
					var he *HTTPError
					if errors.As(e, &he) && he.Status == 409 {
						break
					}
					return e
				}
				local := make([]syncwire.Deletion, len(batch))
				copy(local, batch)
				for i := range local {
					local[i].Heads = nil
				}
				if e = c.Store.Erase(local, false); e != nil {
					return e
				}
			}
		}

		if e = c.Store.PruneDeleted(c.Master); e != nil {
			return e
		}
		if len(expired) > 0 {
			if e = c.Store.Compact(); e != nil {
				return e
			}
		}
		if pending, e := c.Store.deletionRecords(true); e != nil {
			return e
		} else if len(pending) > 0 || len(expired) > 0 {
			return c.syncDeletions(ctx)
		}
		return nil
	}
	return nil
}
func (c *Client) push(ctx context.Context, rev syncwire.Revision) error {
	m, file, key, e := c.Store.unlock(c.Master, rev)
	if e != nil {
		return e
	}
	vc.Wipe(key[:])
	c.report(syncwire.Progress{Phase: "verifying", Revision: rev.ID, Name: m.Name, Direction: "upload", TotalBytes: m.Size, Chunks: m.ChunkCount})
	origin, err := c.Store.Origin(rev.ID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, originKey{}, origin)
	if m.Type == "file" {
		if e = c.Store.ExportContext(ctx, c.Master, rev.ID, io.Discard); e != nil {
			return e
		}
		c.phase("uploading")

		present := map[uint64]bool{}
		after := int64(-1)
		for {
			path := "/v1/blobs/" + file.String() + "/chunks"
			if after >= 0 {
				path += "?after=" + strconv.FormatInt(after, 10)
			}
			b, e := c.request(ctx, "GET", path, nil, 32768)
			if e != nil {
				return e
			}
			var page []uint64
			if parseJSON(b, &page) != nil || len(page) > 1000 {
				return ErrInvalid
			}
			for _, index := range page {
				if index >= m.ChunkCount || int64(index) <= after {
					return ErrInvalid
				}
				present[index] = true
				after = int64(index)
			}
			if len(page) < 1000 {
				break
			}
		}
		e = chunkWindows(ctx, m.ChunkCount, func(ctx context.Context, index uint64) (bool, error) {
			if present[index] {
				return true, nil
			}
			chunk, e := c.Store.chunk(file.String(), index)
			if e != nil {
				return false, e
			}
			_, e = c.request(ctx, "PUT", "/v1/blobs/"+file.String()+"/chunks/"+strconv.FormatUint(index, 10), chunk, 1024)
			return false, e
		}, func(index uint64, reused bool) error {
			c.updateProgress(func(p *syncwire.Progress) {
				p.Chunk = index + 1
				p.DoneBytes = min(m.Size, (index+1)*vc.ChunkSize)
				if reused {
					p.ReusedBytes += min(uint64(vc.ChunkSize), m.Size-index*vc.ChunkSize)
				}
			})
			return nil
		})
		if e != nil {
			return e
		}
	}
	c.phase("committing")
	b, e := c.request(ctx, "PUT", "/v1/sync/revisions/"+rev.ID, rev, 1024)
	if e != nil {
		return e
	}
	var ack syncwire.Cursor
	if parseJSON(b, &ack) != nil || ack.Sequence <= 0 {
		return ErrInvalid
	}
	c.phase("complete")
	return nil
}
