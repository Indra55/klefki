package sync

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"slices"
	"strconv"
	"time"

	"klefki/core/syncwire"
)

// NeedsSync checks durable receipts without replaying local history on idle polls.
func (c *Client) NeedsSync(ctx context.Context) (bool, error) {
	pending, e := c.Store.Pending()
	if e != nil || pending > 0 {
		return true, e
	}
	local, e := c.Store.RelayState()
	if e != nil {
		return true, e
	}
	cursor, e := c.Store.Cursor()
	if e != nil {
		return true, e
	}
	settings, e := c.Settings(ctx)
	if e != nil {
		return true, e
	}
	if local.Mode != settings.Mode || local.Epoch != settings.Epoch {
		return true, nil
	}
	status, e := c.Remote.Status(ctx, c.Session)
	if e != nil {
		return true, e
	}
	if status.Epoch != local.Epoch || status.LatestSequence != cursor || len(status.Devices) != len(local.Known) {
		return true, nil
	}
	for _, peer := range status.Devices {
		if !slices.Contains(local.Known, peer.Device) || (peer.Device == local.Device && peer.Sequence != cursor) || (settings.Mode == syncwire.TemporaryRelay && settings.Queued == 0 && peer.Sequence < cursor) {
			return true, nil
		}
	}
	return false, nil
}

type originKey struct{}
type readProgressKey struct{}

func (s *Store) Origin(revision string) (string, error) {
	var origin string
	err := s.db.QueryRow(`SELECT origin FROM revision_sources WHERE revision=?`, revision).Scan(&origin)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return origin, err
}

func (r *Remote) Presence(ctx context.Context, session Session, in syncwire.Presence) error {
	_, err := r.request(ctx, "POST", "/v1/sync/presence", session.Token, in, 1024)
	return err
}

func (r *Remote) Status(ctx context.Context, session Session) (syncwire.Status, error) {
	var out syncwire.Status
	b, err := r.request(ctx, "GET", "/v1/sync/status", session.Token, nil, 32768)
	if err == nil {
		err = parseJSON(b, &out)
	}
	if err == nil {
		if _, e := parseID(out.Epoch); e != nil || out.LatestSequence < 0 || len(out.Devices) > 64 {
			return out, ErrInvalid
		}
		for _, d := range out.Devices {
			if _, e := parseID(d.Device); e != nil || d.Sequence < 0 || d.Sequence > out.LatestSequence || d.Outstanding < 0 || d.DoneBytes > d.TotalBytes {
				return out, ErrInvalid
			}
		}
	}
	return out, err
}

func (c *Client) provenance(ctx context.Context, after int64) map[string]syncwire.Provenance {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out := map[string]syncwire.Provenance{}
	b, err := c.Remote.request(ctx, "GET", "/v1/sync/provenance?after="+strconv.FormatInt(after, 10), c.Session.Token, nil, 8192)
	var page syncwire.ProvenancePage
	if err != nil || parseJSON(b, &page) != nil || len(page.Changes) > syncwire.PageSize {
		return out
	}
	for _, p := range page.Changes {
		if _, err := parseID(p.Revision); err != nil {
			return map[string]syncwire.Provenance{}
		}
		if p.Origin != "" {
			if _, err := parseID(p.Origin); err != nil {
				return map[string]syncwire.Provenance{}
			}
		}
		if p.Publisher != "" {
			if _, err := parseID(p.Publisher); err != nil {
				return map[string]syncwire.Provenance{}
			}
		}
		if p.Sequence <= after {
			return map[string]syncwire.Provenance{}
		}
		out[p.Revision] = p
	}
	return out
}

func (c *Client) report(p syncwire.Progress) {
	p.UpdatedAt = time.Now().UnixMilli()
	c.progress = p
	if c.Observe != nil {
		c.Observe(p)
	}
}

func (c *Client) phase(phase string) {
	p := c.progress
	p.Phase, p.Attempt, p.RetryAt = phase, 0, 0
	c.report(p)
}

type progressReader struct {
	reader io.Reader
	done   uint64
	report func(uint64)
	last   time.Time
}

func (r *progressReader) Read(b []byte) (int, error) {
	n, err := r.reader.Read(b)
	r.done += uint64(n)
	if n > 0 && (time.Since(r.last) >= 250*time.Millisecond || err != nil) {
		r.last = time.Now()
		r.report(r.done)
	}
	return n, err
}

func StatusUnsupported(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && (he.Status == 404 || he.Status == 405)
}
