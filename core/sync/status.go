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
	var deletionCursor int64
	if e = c.Store.db.QueryRow(`SELECT cursor FROM deletion_state WHERE id=1`).Scan(&deletionCursor); e != nil {
		return true, e
	}
	var contentVersion int64
	if c.Store.db.QueryRow(`SELECT content_version FROM content_policy WHERE id=1`).Scan(&contentVersion) != nil {
		return true, ErrStore
	}
	if contentVersion != settings.ContentVersion {
		return true, nil
	}
	policy, err := c.Store.TrashPolicy()
	if err != nil {
		return true, err
	}
	if settings.TrashPolicyVersion != policy.Version {
		return true, nil
	}
	if settings.DeletionSequence != deletionCursor {
		return true, nil
	}
	if expired, err := c.Store.ExpiredTrash(c.Master, time.Now()); err != nil {
		return true, err
	} else if len(expired) > 0 {
		return true, nil
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
	b, err := r.request(ctx, "GET", "/v1/sync/status?deviceState=1", session.Token, nil, 32768)
	if err == nil {
		err = parseJSON(b, &out)
	}
	if err == nil {
		if _, e := parseID(out.Epoch); e != nil || out.LatestSequence < 0 || len(out.Devices) > 64 {
			return out, ErrInvalid
		}
		for _, d := range out.Devices {
			if _, e := parseID(d.Device); e != nil || d.Sequence < 0 || d.Sequence > out.LatestSequence || d.Outstanding < 0 || d.DoneBytes > d.TotalBytes || (d.DeviceState != nil && (d.DeviceState.Version < 1 || d.DeviceState.Version > syncwire.MaxDeviceStateVersion || d.DeviceState.ReportedAt < 0)) {
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
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	p.UpdatedAt = time.Now().UnixMilli()
	c.progress = p
	if c.Observe != nil {
		c.Observe(p)
	}
}

func (c *Client) phase(phase string) {
	p := c.progressSnapshot()
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

func (c *Client) progressSnapshot() syncwire.Progress {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	return c.progress
}
func (c *Client) updateProgress(update func(*syncwire.Progress)) {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	update(&c.progress)
	c.progress.UpdatedAt = time.Now().UnixMilli()
	if c.Observe != nil {
		c.Observe(c.progress)
	}
}

// DeviceState snapshots pending work and its report version in one transaction.
func (s *Store) DeviceState(minimum int64) (syncwire.DeviceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out syncwire.DeviceState
	tx, err := s.db.Begin()
	if err != nil {
		return out, ErrStore
	}
	defer tx.Rollback()
	var pending int
	var previous bool
	if tx.QueryRow(pendingWorkQuery).Scan(&pending) != nil || tx.QueryRow(`SELECT version,pending FROM local_device_state WHERE id=1`).Scan(&out.Version, &previous) != nil {
		return out, ErrStore
	}
	out.Pending = pending > 0
	if out.Version == 0 || previous != out.Pending || out.Version < minimum {
		out.Version = max(out.Version+1, minimum)
		if out.Version > syncwire.MaxDeviceStateVersion {
			return out, ErrInvalid
		}
		if _, err = tx.Exec(`UPDATE local_device_state SET version=?,pending=? WHERE id=1`, out.Version, out.Pending); err != nil {
			return out, ErrStore
		}
	}
	if tx.Commit() != nil {
		return out, ErrStore
	}
	return out, nil
}

// ReportDeviceState sends only changed reports; receipt polling supplies capability and ordering.
func (c *Client) ReportDeviceState(ctx context.Context, status syncwire.Status) (*syncwire.DeviceState, error) {
	if !status.DeviceStateSupported {
		return nil, nil
	}
	local, err := c.Store.RelayState()
	if err != nil {
		return nil, err
	}
	var peer *syncwire.DeviceStatus
	for i := range status.Devices {
		if status.Devices[i].Device == local.Device {
			peer = &status.Devices[i]
			break
		}
	}
	if peer == nil {
		return nil, nil
	}
	report, err := c.Store.DeviceState(0)
	if err != nil {
		return nil, err
	}
	if previous := peer.DeviceState; previous != nil {
		if previous.Version == report.Version && previous.Pending == report.Pending {
			return nil, nil
		}
		if previous.Version >= report.Version {
			report, err = c.Store.DeviceState(previous.Version + 1)
			if err != nil {
				return nil, err
			}
		}
	}
	b, err := c.Remote.request(ctx, "POST", "/v1/sync/device-state", c.Session.Token, syncwire.DeviceStateUpdate{Device: local.Device, Version: report.Version, Pending: report.Pending}, 1024)
	var out syncwire.DeviceState
	if err == nil {
		err = parseJSON(b, &out)
	}
	if err == nil && (out.Version != report.Version || out.Pending != report.Pending || out.ReportedAt < 0) {
		err = ErrInvalid
	}
	return &out, err
}
