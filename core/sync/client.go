package sync

import (
	"bytes"
	"context"
	"database/sql"
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
	Store     *Store
	Remote    *Remote
	Session   Session
	Master    vc.Key
	Retry     Retry
	Observe   func(syncwire.Progress)
	OnContact func()
	progress  syncwire.Progress
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
		b, e := c.Remote.request(ctx, method, path, c.Session.Token, body, limit)
		if e == nil {
			if c.OnContact != nil {
				c.OnContact()
			}
			return b, nil
		}
		retry := errors.Is(e, ErrRemote)
		wait := delay
		if h, ok := e.(*HTTPError); ok {
			retry = h.Status == 408 || h.Status == 429 || h.Status >= 500 && h.Status != 507
			wait = max(wait, h.retryAfter)
		}
		if !retry || attempt+1 >= attempts {
			return nil, e
		}
		previous := c.progress
		p := previous
		p.Phase, p.Attempt, p.RetryAt = "retrying", attempt+2, time.Now().Add(wait).UnixMilli()
		c.report(p)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		c.report(previous)
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
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if e := c.Store.bind(c.Remote.base); e != nil {
		return e
	}
	var seedErr error
	if e := c.prepareRelay(ctx, true); e != nil {
		var he *HTTPError
		if !errors.As(e, &he) || he.Status != 507 {
			return e
		}
		seedErr = e
	}
	for seedErr == nil {
		rev, e := c.Store.nextPending()
		if e == sql.ErrNoRows {
			break
		}
		if e != nil {
			return ErrStore
		}
		if e = c.push(ctx, rev); e != nil {
			return e
		}
		if e = c.Store.uploaded(rev.ID); e != nil {
			return e
		}
	}
	for {
		cursor, e := c.Store.Cursor()
		if e != nil {
			return e
		}
		b, e := c.request(ctx, "GET", "/v1/sync/changes?after="+strconv.FormatInt(cursor, 10), nil, syncwire.MaxPageBytes)
		if e != nil {
			return e
		}
		var page syncwire.Page
		if parseJSON(b, &page) != nil || len(page.Changes) > syncwire.PageSize {
			return ErrInvalid
		}
		origins := map[string]syncwire.Provenance{}
		if len(page.Changes) > 0 {
			origins = c.provenance(ctx, cursor)
		}
		for _, change := range page.Changes {
			if change.Sequence <= cursor {
				return ErrInvalid
			}
			if len(change.Revision.Metadata) == 0 {
				local, e := c.Store.localRevision(change.Revision.ID)
				if e != nil {
					if errors.Is(e, ErrRelayWaiting) {
						if _, ackErr := c.request(ctx, "PUT", "/v1/sync/cursor", syncwire.Cursor{Sequence: cursor}, 1024); ackErr != nil {
							return ackErr
						}
					}
					return e
				}
				if local.Object != change.Revision.Object || local.Base != change.Revision.Base {
					return ErrInvalid
				}
				change.Revision = local
			}
			m, _, key, err := c.Store.unlock(c.Master, change.Revision)
			vc.Wipe(key[:])
			if err != nil {
				return err
			}
			origin := origins[change.Revision.ID]
			c.report(syncwire.Progress{Phase: "downloading", Revision: change.Revision.ID, Name: m.Name, Direction: "download", Source: origin.Origin, Publisher: origin.Publisher, TotalBytes: m.Size, Chunks: m.ChunkCount})
			e = c.Store.applyTracked(ctx, c.Master, change, origin.Origin, func(done, chunk uint64, reused bool) {
				p := c.progress
				if reused {
					p.ReusedBytes += min(uint64(vc.ChunkSize), m.Size-(chunk-1)*vc.ChunkSize)
				}
				p.DoneBytes, p.Chunk = done, chunk
				if chunk == m.ChunkCount {
					p.Phase = "committing"
				}
				c.report(p)
			}, func(file string, index uint64) ([]byte, error) {
				base := index * vc.ChunkSize
				p := c.progress
				p.Chunk = index + 1
				c.report(p)
				readCtx := context.WithValue(ctx, readProgressKey{}, func(n uint64) {
					p := c.progress
					if n > 87 {
						p.DoneBytes = base + min(n-87, m.Size-base)
						c.report(p)
					}
				})
				return c.request(readCtx, "GET", "/v1/blobs/"+file+"/chunks/"+strconv.FormatUint(index, 10), nil, vc.ChunkSize+87)
			})
			if e != nil {
				return e
			}
			cursor = change.Sequence
			c.phase("complete")
		}
		// Acknowledgement follows the durable local transaction, including all chunks.
		c.report(syncwire.Progress{Phase: "acknowledging"})
		if _, e = c.request(ctx, "PUT", "/v1/sync/cursor", syncwire.Cursor{Sequence: cursor}, 1024); e != nil {
			return e
		}
		if len(page.Changes) == 0 {
			return seedErr
		}
	}
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
		// Keep only one inventory page in memory, and skip already accepted chunks.
		var page []uint64
		position := 0
		after := int64(-1)
		more := true
		for index := uint64(0); index < m.ChunkCount; index++ {
			if position == len(page) && more {
				path := "/v1/blobs/" + file.String() + "/chunks"
				if after >= 0 {
					path += "?after=" + strconv.FormatInt(after, 10)
				}
				b, e := c.request(ctx, "GET", path, nil, 32768)
				if e != nil {
					return e
				}
				if parseJSON(b, &page) != nil || len(page) > 1000 {
					return ErrInvalid
				}
				position = 0
				last := after
				for _, n := range page {
					if n >= m.ChunkCount || int64(n) <= last {
						return ErrInvalid
					}
					last = int64(n)
				}
				after = last
				more = len(page) == 1000
			}
			if position < len(page) && page[position] == index {
				position++
				p := c.progress
				p.Chunk = index + 1
				p.DoneBytes = min(m.Size, (index+1)*vc.ChunkSize)
				p.ReusedBytes += min(uint64(vc.ChunkSize), m.Size-index*vc.ChunkSize)
				c.report(p)
				continue
			}
			chunk, e := c.Store.chunk(file.String(), index)
			if e != nil {
				return e
			}
			p := c.progress
			p.Chunk = index + 1
			c.report(p)
			if _, e = c.request(ctx, "PUT", "/v1/blobs/"+file.String()+"/chunks/"+strconv.FormatUint(index, 10), chunk, 1024); e != nil {
				return e
			}
			p = c.progress
			p.DoneBytes = min(m.Size, (index+1)*vc.ChunkSize)
			c.report(p)
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
