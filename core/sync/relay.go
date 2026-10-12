package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

var ErrRelayWaiting = errors.New("waiting for another device to supply relay data")
var ErrRelayUnsupported = errors.New("server does not support temporary relay")

type LocalRelay struct {
	Device string
	Epoch  string
	Mode   string
	Known  []string
}

func (s *Store) RelayState() (LocalRelay, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.db.Exec(`CREATE TABLE IF NOT EXISTS relay_local(id INTEGER PRIMARY KEY CHECK(id=1),device TEXT NOT NULL,epoch TEXT NOT NULL,mode TEXT NOT NULL,known TEXT NOT NULL)`); e != nil {
		return LocalRelay{}, ErrStore
	}
	id, e := vc.RandomID()
	if e != nil {
		return LocalRelay{}, e
	}
	if _, e = s.db.Exec(`INSERT OR IGNORE INTO relay_local VALUES(1,?,'','persistent','[]')`, id.String()); e != nil {
		return LocalRelay{}, ErrStore
	}
	var out LocalRelay
	var known string
	if s.db.QueryRow(`SELECT device,epoch,mode,known FROM relay_local WHERE id=1`).Scan(&out.Device, &out.Epoch, &out.Mode, &known) != nil || json.Unmarshal([]byte(known), &out.Known) != nil {
		return out, ErrStore
	}
	return out, nil
}

func (s *Store) saveRelay(state syncwire.RelayState) error {
	if state.Mode != syncwire.Persistent && state.Mode != syncwire.TemporaryRelay {
		return ErrInvalid
	}
	if _, e := vc.ParseID(state.Epoch); e != nil {
		return ErrInvalid
	}
	if len(state.Devices) > 64 {
		return ErrInvalid
	}
	for _, id := range state.Devices {
		if _, e := vc.ParseID(id); e != nil {
			return ErrInvalid
		}
	}
	known, _ := json.Marshal(state.Devices)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return ErrStore
	}
	defer tx.Rollback()
	var old string
	if tx.QueryRow(`SELECT epoch FROM relay_local WHERE id=1`).Scan(&old) != nil {
		return ErrStore
	}
	if old != state.Epoch {
		if _, e = tx.Exec(`UPDATE local_state SET cursor=0 WHERE singleton=1`); e != nil {
			return ErrStore
		}
	}
	if _, e = tx.Exec(`UPDATE relay_local SET epoch=?,mode=?,known=? WHERE id=1`, state.Epoch, state.Mode, string(known)); e != nil {
		return ErrStore
	}
	if tx.Commit() != nil {
		return ErrStore
	}
	return nil
}

func (r *Remote) LoginAccount(ctx context.Context, a vc.Account, credential vc.Key, restore bool) (Session, error) {
	defer vc.Wipe(credential[:])
	session, e := r.Login(ctx, a.ID, credential)
	var he *HTTPError
	if !restore || !errors.As(e, &he) || he.Status != 401 {
		return session, e
	}
	_, lookup := r.request(ctx, "GET", "/v1/accounts/"+a.ID.String(), "", nil, 8192)
	if !errors.As(lookup, &he) || he.Status != 404 {
		return session, e
	}
	session, e = r.Signup(ctx, a, credential)
	if errors.As(e, &he) && he.Status == 409 {
		return r.Login(ctx, a.ID, credential)
	}
	if e == nil {
		_, e = r.request(ctx, "PUT", "/v1/sync/settings", session.Token, struct {
			Mode string `json:"mode"`
		}{syncwire.TemporaryRelay}, 1024)
	}
	return session, e
}

func (c *Client) Settings(ctx context.Context) (syncwire.SyncSettings, error) {
	b, e := c.request(ctx, "GET", "/v1/sync/settings", nil, 1024)
	if e != nil {
		return syncwire.SyncSettings{}, e
	}
	var settings syncwire.SyncSettings
	if parseJSON(b, &settings) != nil || (settings.Mode != syncwire.Persistent && settings.Mode != syncwire.TemporaryRelay) {
		return settings, ErrInvalid
	}
	if _, e = vc.ParseID(settings.Epoch); e != nil {
		return settings, ErrInvalid
	}
	return settings, nil
}
func (c *Client) SetMode(ctx context.Context, mode string) error {
	if mode != syncwire.Persistent && mode != syncwire.TemporaryRelay {
		return ErrInvalid
	}
	c.Store.syncMu.Lock()
	defer c.Store.syncMu.Unlock()
	_, e := c.request(ctx, "PUT", "/v1/sync/settings", struct {
		Mode string `json:"mode"`
	}{mode}, 1024)
	if e != nil {
		var he *HTTPError
		if errors.As(e, &he) && he.Status == 404 {
			return ErrRelayUnsupported
		}
		return e
	}
	return c.prepareRelay(ctx, false)
}

func (c *Client) ForgetDevice(ctx context.Context, id string) error {
	if _, e := vc.ParseID(id); e != nil {
		return ErrInvalid
	}
	c.Store.syncMu.Lock()
	defer c.Store.syncMu.Unlock()
	if e := c.prepareRelay(ctx, false); e != nil {
		return e
	}
	if _, e := c.request(ctx, "DELETE", "/v1/sync/relay/devices/"+id, nil, 1024); e != nil {
		return e
	}
	return c.prepareRelay(ctx, false)
}

func (c *Client) SyncAccount(ctx context.Context, a vc.Account, credential vc.Key) error {
	defer vc.Wipe(credential[:])
	e := c.Sync(ctx)
	var he *HTTPError
	if !errors.As(e, &he) || he.Status != 401 {
		return e
	}
	local, err := c.Store.RelayState()
	if err != nil {
		return err
	}
	if local.Mode != syncwire.TemporaryRelay {
		return e
	}
	c.Session, err = c.Remote.LoginAccount(ctx, a, credential, true)
	if err != nil {
		return err
	}
	return c.Sync(ctx)
}

func (c *Client) prepareRelay(ctx context.Context, seed bool) error {
	local, e := c.Store.RelayState()
	if e != nil {
		return e
	}

	cursor, e := c.Store.Cursor()
	if e != nil {
		return e
	}
	var after int64
	ids := []string{}
	if seed {
		ids, e = c.Store.relayIDs(&after, syncwire.RelayBatchSize)
		if e != nil {
			return e
		}
	}
	device := syncwire.RelayDevice{Device: local.Device, Known: local.Known, Sequence: cursor}
	b, e := c.request(ctx, "POST", "/v1/sync/relay/prepare", syncwire.RelayPrepare{RelayDevice: device, IDs: ids}, 16384)
	modern := true
	var prepared syncwire.RelayPrepared
	var state syncwire.RelayState
	if unsupportedRelayEndpoint(e) {
		modern = false
		_, e = c.Settings(ctx)
		if e != nil {
			if unsupportedRelayEndpoint(e) && local.Mode != syncwire.TemporaryRelay {
				return nil
			}
			return e
		}
		b, e = c.request(ctx, "POST", "/v1/sync/relay/device", device, 8192)
		if e != nil {
			return e
		}
		if parseJSON(b, &state) != nil {
			return ErrInvalid
		}
	} else {
		if e != nil {
			return e
		}
		if parseJSON(b, &prepared) != nil {
			return ErrInvalid
		}
		state = prepared.RelayState
	}
	if !slices.Contains(state.Devices, local.Device) {
		return ErrInvalid
	}
	if e = c.Store.saveRelay(state); e != nil {
		return e
	}
	c.Remote.deviceMu.Lock()
	c.Remote.device = local.Device
	c.Remote.deviceMu.Unlock()
	if !seed {
		return nil
	}

	size, path, limit := syncwire.RelayBatchSize, "/v1/sync/relay/need-batch", int64(16384)
	if !modern {
		size, path, limit = syncwire.PageSize, "/v1/sync/relay/need", 2048
		after = 0
		ids, e = c.Store.relayIDs(&after, size)
		if e != nil {
			return e
		}
	}
	first := modern
	for len(ids) > 0 {
		needed := syncwire.RelayNeed{IDs: prepared.IDs}
		if !first {
			b, e = c.request(ctx, "POST", path, syncwire.RelayNeed{IDs: ids}, limit)
			if unsupportedRelayEndpoint(e) && modern {
				// A mixed-version deployment can support prepare before need-batch.
				modern = false
				size, path, limit = syncwire.PageSize, "/v1/sync/relay/need", 2048
				after = 0
				ids, e = c.Store.relayIDs(&after, size)
				if e != nil {
					return e
				}
				continue
			}
			if e != nil {
				return e
			}
			if parseJSON(b, &needed) != nil {
				return ErrInvalid
			}
		}
		first = false
		set := map[string]bool{}
		for _, id := range needed.IDs {
			if !slices.Contains(ids, id) || set[id] {
				return ErrInvalid
			}
			set[id] = true
		}
		for _, id := range ids {
			if set[id] {
				rev, e := c.Store.localRevision(id)
				if e != nil {
					return e
				}
				if e = c.push(ctx, rev); e != nil {
					return e
				}
				if e = c.Store.uploaded(id); e != nil {
					return e
				}
			}
		}
		ids, e = c.Store.relayIDs(&after, size)
		if e != nil {
			return e
		}
	}

	return nil
}

func (s *Store) localRevision(id string) (syncwire.Revision, error) {
	r, e := readRevision(s.db.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, id))
	if e == sql.ErrNoRows {
		return r, ErrRelayWaiting
	}
	if e != nil {
		return r, ErrStore
	}
	return r, nil
}

func (s *Store) acceptExisting(change syncwire.Change) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return false, ErrStore
	}
	defer tx.Rollback()
	old, e := readRevision(tx.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, change.Revision.ID))
	if e == sql.ErrNoRows {
		return false, nil
	}
	if e != nil {
		return false, ErrStore
	}
	if !sameRevision(old, change.Revision) {
		return false, ErrConflict
	}
	result, e := tx.Exec(`UPDATE local_state SET cursor=? WHERE singleton=1 AND cursor<?`, change.Sequence, change.Sequence)
	if e != nil {
		return false, ErrStore
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return false, ErrInvalid
	}
	if _, e = tx.Exec(`UPDATE revisions SET pending=0 WHERE id=?`, old.ID); e != nil {
		return false, ErrStore
	}
	if e = tx.Commit(); e != nil {
		return false, ErrStore
	}
	return true, nil
}

func unsupportedRelayEndpoint(e error) bool {
	var he *HTTPError
	return errors.As(e, &he) && (he.Status == 404 || he.Status == 405)
}
func (s *Store) relayIDs(after *int64, limit int) ([]string, error) {
	rows, e := s.db.Query(`SELECT ordinal,id FROM revisions WHERE ordinal>? ORDER BY ordinal LIMIT ?`, *after, limit)
	if e != nil {
		return nil, ErrStore
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(after, &id) != nil {
			return nil, ErrStore
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil, ErrStore
	}
	return ids, nil
}
