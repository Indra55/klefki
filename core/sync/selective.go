package sync

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strconv"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

const pendingWorkQuery = `SELECT (SELECT COUNT(*) FROM revisions r WHERE r.pending=1 OR EXISTS(SELECT 1 FROM content_work w WHERE w.revision=r.id) OR EXISTS(SELECT 1 FROM deferred_content d WHERE d.revision=r.id AND d.wanted=1) OR EXISTS(SELECT 1 FROM sync_tasks t,sync_queue q WHERE q.active=1 AND t.revision=r.id))+(SELECT COUNT(*) FROM deletions WHERE pending=1)`

var ErrContentUnsupported = errors.New("UPDATE_REQUIRED: server does not support selective content sync")
var ErrContentUnavailable = errors.New("CONTENT_UNAVAILABLE: reconnect a device holding this file and enable Trash sync or restore it there")

var errContentReplan = errors.New("content plan changed")

type contentGuardKey struct{}
type metadataOnlyKey struct{}
type hydrateKey struct{}

type TrashPolicy struct {
	Override  string `json:"override"`
	Default   bool   `json:"default"`
	Effective bool   `json:"effective"`
	Supported bool   `json:"supported"`
	Version   int64  `json:"version"`
}

func (s *Store) migrateContent() error {
	_, e := s.db.Exec(`CREATE TABLE IF NOT EXISTS content_policy(id INTEGER PRIMARY KEY CHECK(id=1),override TEXT NOT NULL DEFAULT 'follow-vault',enabled INTEGER NOT NULL DEFAULT 0,version INTEGER NOT NULL DEFAULT 0,metadata_cursor INTEGER NOT NULL DEFAULT 0,content_version INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO content_policy(id) VALUES(1);
 CREATE TABLE IF NOT EXISTS deferred_content(revision TEXT PRIMARY KEY,wanted INTEGER NOT NULL DEFAULT 0,explicit INTEGER NOT NULL DEFAULT 0,trash INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS metadata_receipts(revision TEXT PRIMARY KEY,sequence INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS content_work(revision TEXT PRIMARY KEY);
 CREATE TRIGGER IF NOT EXISTS content_revision_delete AFTER DELETE ON revisions BEGIN DELETE FROM deferred_content WHERE revision=OLD.id; DELETE FROM content_work WHERE revision=OLD.id; END;`)
	return e
}
func (s *Store) TrashPolicy() (TrashPolicy, error) {
	var p TrashPolicy
	if s.db.QueryRow(`SELECT override,enabled,version FROM content_policy WHERE id=1`).Scan(&p.Override, &p.Default, &p.Version) != nil {
		return p, ErrStore
	}
	p.Supported = p.Version > 0
	p.Effective = p.Override == "on" || p.Override == "follow-vault" && p.Default
	return p, nil
}
func (s *Store) SetTrashOverride(value string) error {
	if value != "on" && value != "off" && value != "follow-vault" {
		return ErrInvalid
	}
	_, e := s.db.Exec(`UPDATE content_policy SET override=? WHERE id=1`, value)
	return e
}
func (c *Client) SetTrashDefault(ctx context.Context, enabled bool) error {
	settings, e := c.Settings(ctx)
	if e != nil {
		return e
	}
	if !settings.SelectiveContent {
		return ErrContentUnsupported
	}
	_, e = c.request(ctx, "PUT", "/v1/sync/trash-policy", struct {
		Enabled bool `json:"enabled"`
	}{enabled}, 1024)
	if e != nil {
		return e
	}
	latest, e := c.Settings(ctx)
	if e != nil {
		return e
	}
	_, e = c.Store.db.Exec(`UPDATE content_policy SET enabled=?,version=? WHERE id=1`, latest.TrashDefault, latest.TrashPolicyVersion)
	return e
}
func (s *Store) ContentAvailable(master vc.Key, revision string) (bool, error) {
	defer vc.Wipe(master[:])
	r, e := s.localRevision(revision)
	if e != nil {
		return false, e
	}
	m, file, key, e := s.unlock(master, r)
	vc.Wipe(key[:])
	if e != nil {
		return false, e
	}
	if m.Type != "file" {
		return true, nil
	}
	var count uint64
	if s.db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE file=?`, file.String()).Scan(&count) != nil {
		return false, ErrStore
	}
	return count == m.ChunkCount, nil
}

// Classify objects using every current branch and ancestor, not historical
// trashed_at values. An active conflict branch keeps its history eligible.
func (s *Store) trashObjects(master vc.Key) (map[string]bool, error) {
	defer vc.Wipe(master[:])
	heads, e := s.allHeads(master)
	if e != nil {
		return nil, e
	}
	byObject := map[string][]Head{}
	for _, h := range heads {
		byObject[h.Object] = append(byObject[h.Object], h)
	}
	var trashed func(Head, map[string]bool) bool
	trashed = func(h Head, seen map[string]bool) bool {
		if trashRoot(h.Metadata) {
			return true
		}
		if h.Metadata.Parent == nil || seen[h.Object] {
			return false
		}
		seen[h.Object] = true
		parents := byObject[*h.Metadata.Parent]
		if len(parents) == 0 {
			return false
		}
		for _, p := range parents {
			copy := map[string]bool{}
			for k, v := range seen {
				copy[k] = v
			}
			if !trashed(p, copy) {
				return false
			}
		}
		return true
	}
	out := map[string]bool{}
	for object, branches := range byObject {
		out[object] = true
		for _, h := range branches {
			if !trashed(h, map[string]bool{}) {
				out[object] = false
			}
		}
	}
	return out, nil
}
func (s *Store) contentHistory() ([]syncwire.Revision, error) {
	rows, e := s.db.Query(`SELECT id,object,base,metadata,file_key FROM revisions ORDER BY ordinal`)
	if e != nil {
		return nil, ErrStore
	}
	defer rows.Close()
	out := []syncwire.Revision{}
	for rows.Next() {
		var r syncwire.Revision
		if rows.Scan(&r.ID, &r.Object, &r.Base, &r.Metadata, &r.FileKey) != nil {
			return nil, ErrStore
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (c *Client) contentAction(ctx context.Context, action string, ids []string) error {
	for start := 0; start < len(ids); start += syncwire.RelayBatchSize {
		if _, e := c.request(ctx, "POST", "/v1/sync/content", syncwire.ContentAction{Action: action, IDs: ids[start:min(start+syncwire.RelayBatchSize, len(ids))]}, 1024); e != nil {
			return e
		}
	}
	return nil
}
func (c *Client) contentStates(ctx context.Context, ids []string) (map[string]syncwire.ContentState, error) {
	out := map[string]syncwire.ContentState{}
	for start := 0; start < len(ids); start += syncwire.RelayBatchSize {
		batch := ids[start:min(start+syncwire.RelayBatchSize, len(ids))]
		b, e := c.request(ctx, "POST", "/v1/sync/content/state", syncwire.ContentBatch{IDs: batch}, 32768)
		if e != nil {
			return nil, e
		}
		var states syncwire.ContentStates
		if parseJSON(b, &states) != nil || len(states.States) != len(batch) {
			return nil, ErrInvalid
		}
		for i, state := range states.States {
			if state.ID != batch[i] {
				return nil, ErrInvalid
			}
			out[state.ID] = state
		}
	}
	return out, nil
}

func (c *Client) syncSelective(ctx context.Context, settings syncwire.SyncSettings) error {
	if e := c.Store.bind(c.Remote.base); e != nil {
		return e
	}
	if e := c.syncDeletions(ctx); e != nil {
		return e
	}
	if e := c.prepareRelay(ctx, false); e != nil {
		return e
	}
	if _, e := c.request(ctx, "POST", "/v1/sync/content/activate", nil, 1024); e != nil {
		return e
	}
	if _, e := c.Store.db.Exec(`UPDATE content_policy SET enabled=?,version=? WHERE id=1 AND version<=?`, settings.TrashDefault, settings.TrashPolicyVersion, settings.TrashPolicyVersion); e != nil {
		return ErrStore
	}
	history, e := c.Store.contentHistory()
	if e != nil {
		return e
	}
	localTrash, e := c.Store.trashObjects(c.Master)
	if e != nil {
		return e
	}
	policy, e := c.Store.TrashPolicy()
	if e != nil {
		return e
	}
	// Metadata history is small and dependency ordered. It must precede content
	// selection so an old live revision cannot hide a later move into Trash.
	for start := 0; start < len(history); start += syncwire.RelayBatchSize {
		batch := history[start:min(start+syncwire.RelayBatchSize, len(history))]
		ids := []string{}
		for _, r := range batch {
			ids = append(ids, r.ID)
		}
		b, e := c.request(ctx, "POST", "/v1/sync/relay/need-batch", syncwire.RelayNeed{IDs: ids}, 16384)
		if e != nil {
			if unsupportedRelayEndpoint(e) {
				return ErrContentUnsupported
			}
			return e
		}
		var needed syncwire.RelayNeed
		if parseJSON(b, &needed) != nil {
			return ErrInvalid
		}
		set := map[string]bool{}
		for _, id := range needed.IDs {
			if set[id] {
				return ErrInvalid
			}
			set[id] = true
		}
		for id := range set {
			found := false
			for _, r := range batch {
				found = found || r.ID == id
			}
			if !found {
				return ErrInvalid
			}
		}
		for _, r := range batch {
			if set[r.ID] && len(r.FileKey) > 0 && (!localTrash[r.Object] || policy.Effective) {
				have, e := c.Store.ContentAvailable(c.Master, r.ID)
				if e != nil {
					return e
				}
				if have {
					if _, e = c.Store.db.Exec(`INSERT OR IGNORE INTO content_work VALUES(?)`, r.ID); e != nil {
						return ErrStore
					}
				}
			}
			if set[r.ID] {
				origin, e := c.Store.Origin(r.ID)
				if e != nil {
					return e
				}
				if _, e = c.request(context.WithValue(ctx, originKey{}, origin), "PUT", "/v1/sync/revisions/"+r.ID, r, 1024); e != nil {
					return e
				}
			}
			if e = c.Store.uploaded(r.ID); e != nil {
				return e
			}
		}
	}
	var through *int64
	for {
		var cursor int64
		e := c.Store.db.QueryRow(`SELECT metadata_cursor FROM content_policy WHERE id=1`).Scan(&cursor)
		if e != nil {
			return e
		}
		path := "/v1/sync/changes?after=" + strconv.FormatInt(cursor, 10)
		if through != nil {
			path += "&through=" + strconv.FormatInt(*through, 10)
		}
		b, e := c.request(ctx, "GET", path, nil, syncwire.MaxPageBytes)
		if e != nil {
			return e
		}
		var page syncwire.Page
		if parseJSON(b, &page) != nil || len(page.Changes) > syncwire.PageSize || page.Latest < cursor {
			return ErrInvalid
		}
		if through == nil {
			limit := page.Latest
			through = &limit
		}
		if len(page.Changes) == 0 {
			break
		}
		origins := c.provenance(ctx, cursor)
		for _, change := range page.Changes {
			if change.Sequence <= cursor || change.Sequence > *through {
				return ErrInvalid
			}
			if deleted, e := c.Store.Deleted(change.Revision.Object); e != nil {
				return e
			} else if deleted {
				tx, e := c.Store.db.Begin()
				if e != nil {
					return ErrStore
				}
				if e = recordMetadataReceipt(tx, change); e != nil {
					tx.Rollback()
					return e
				}
				if e = tx.Commit(); e != nil {
					return ErrStore
				}
				cursor = change.Sequence
				continue
			}
			if len(change.Revision.Metadata) == 0 {
				local, e := c.Store.localRevision(change.Revision.ID)
				if e != nil {
					return e
				}
				if local.Object != change.Revision.Object || local.Base != change.Revision.Base {
					return ErrInvalid
				}
				change.Revision = local
			}
			if e = c.Store.applyTracked(context.WithValue(ctx, metadataOnlyKey{}, true), c.Master, change, origins[change.Revision.ID].Origin, nil, nil); e != nil {
				return e
			}
			cursor = change.Sequence
		}
		if cursor >= *through {
			break
		}
	}
	transferErr := c.transferContent(ctx)
	if e = c.advanceContentCursor(); e != nil {
		return e
	}
	cursor, e := c.Store.Cursor()
	if e != nil {
		return e
	}
	if _, e = c.request(ctx, "PUT", "/v1/sync/cursor", syncwire.Cursor{Sequence: cursor}, 1024); e != nil {
		return e
	}
	if transferErr != nil {
		return transferErr
	}
	if e = c.finishTrashSync(ctx); e != nil {
		return e
	}
	_, e = c.Store.db.Exec(`UPDATE content_policy SET content_version=? WHERE id=1`, settings.ContentVersion)
	return e
}

func (c *Client) transferContent(ctx context.Context) error {
	history, e := c.Store.contentHistory()
	if e != nil {
		return e
	}
	trash, e := c.Store.trashObjects(c.Master)
	if e != nil {
		return e
	}
	policy, e := c.Store.TrashPolicy()
	if e != nil {
		return e
	}
	var active, optional []syncwire.Revision
	requests, releases := []string{}, []string{}
	for _, r := range history {
		if len(r.FileKey) == 0 {
			continue
		}
		have, e := c.Store.ContentAvailable(c.Master, r.ID)
		if e != nil {
			return e
		}
		var explicit bool
		e = c.Store.db.QueryRow(`SELECT explicit FROM deferred_content WHERE revision=?`, r.ID).Scan(&explicit)
		if e != nil && e != sql.ErrNoRows {
			return ErrStore
		}
		wanted := !trash[r.Object] || policy.Effective || explicit
		if _, e = c.Store.db.Exec(`UPDATE deferred_content SET wanted=?,trash=? WHERE revision=?`, wanted, trash[r.Object], r.ID); e != nil {
			return ErrStore
		}
		if !wanted {
			releases = append(releases, r.ID)
			if _, e = c.Store.db.Exec(`DELETE FROM content_work WHERE revision=?`, r.ID); e != nil {
				return ErrStore
			}
			continue
		}
		if !have {
			requests = append(requests, r.ID)
		} else {
			if _, e = c.Store.db.Exec(`INSERT OR IGNORE INTO content_work VALUES(?)`, r.ID); e != nil {
				return ErrStore
			}
		}
		if trash[r.Object] && !explicit {
			optional = append(optional, r)
		} else {
			active = append(active, r)
		}
	}
	if e = c.contentAction(ctx, "release", releases); e != nil {
		return e
	}
	if e = c.contentAction(ctx, "request", requests); e != nil {
		return e
	}
	var waiting error
	for priority, group := range [][]syncwire.Revision{active, optional} {
		ids := []string{}
		for _, r := range group {
			ids = append(ids, r.ID)
		}
		states, e := c.contentStates(ctx, ids)
		if e != nil {
			return e
		}
		uploadIDs, received := []string{}, []string{}
		for _, r := range group {
			state := states[r.ID]
			have, e := c.Store.ContentAvailable(c.Master, r.ID)
			if e != nil {
				return e
			}
			if have && !state.Available && state.Requested {
				uploadIDs = append(uploadIDs, r.ID)
			}
		}
		if e = c.contentAction(ctx, "request", uploadIDs); e != nil {
			return e
		}
		for _, r := range group {
			if e = ctx.Err(); e != nil {
				return e
			}
			current, e := c.Store.TrashPolicy()
			if e != nil {
				return e
			}
			if trash[r.Object] && !current.Effective {
				var explicit bool
				_ = c.Store.db.QueryRow(`SELECT explicit FROM deferred_content WHERE revision=?`, r.ID).Scan(&explicit)
				if !explicit {
					return errContentReplan
				}
			}
			fileCtx := ctx
			if priority == 1 {
				queued, e := c.Store.contentHistory()
				if e != nil {
					return e
				}
				for _, q := range queued {
					var pending bool
					if c.Store.db.QueryRow(`SELECT pending FROM revisions WHERE id=?`, q.ID).Scan(&pending) != nil {
						return ErrStore
					}
					if pending && !trash[q.Object] {
						return errContentReplan
					}
				}
				cursor := int64(0)
				if c.Store.db.QueryRow(`SELECT metadata_cursor FROM content_policy WHERE id=1`).Scan(&cursor) != nil {
					return ErrStore
				}
				b, e := c.request(ctx, "GET", "/v1/sync/changes?after="+strconv.FormatInt(cursor, 10), nil, syncwire.MaxPageBytes)
				if e != nil {
					return e
				}
				var page syncwire.Page
				if parseJSON(b, &page) != nil {
					return ErrInvalid
				}
				if len(page.Changes) > 0 {
					return errContentReplan
				}
				fileCtx = context.WithValue(ctx, contentGuardKey{}, func() error {
					p, e := c.Store.TrashPolicy()
					if e != nil {
						return e
					}
					if !p.Effective {
						return errContentReplan
					}
					return nil
				})
			}
			state := states[r.ID]
			have, e := c.Store.ContentAvailable(c.Master, r.ID)
			if e != nil {
				return e
			}
			if have {
				if !state.Available && state.Requested {
					if e = c.push(fileCtx, r); e != nil {
						if errors.Is(e, errContentReplan) {
							if releaseErr := c.contentAction(ctx, "release", []string{r.ID}); releaseErr != nil {
								return releaseErr
							}
						}
						return e
					}
					if e = c.contentAction(ctx, "available", []string{r.ID}); e != nil {
						return e
					}
				}
				received = append(received, r.ID)
				if _, e = c.Store.db.Exec(`DELETE FROM content_work WHERE revision=?`, r.ID); e != nil {
					return ErrStore
				}
			} else if state.Available {
				if e = c.fetchContent(fileCtx, r); e != nil {
					return e
				}
				received = append(received, r.ID)
			} else {
				waiting = ErrContentUnavailable
			}
		}
		if e = c.contentAction(ctx, "received", received); e != nil {
			return e
		}
		if waiting != nil {
			return waiting
		}
	}
	return waiting
}
func (c *Client) fetchContent(ctx context.Context, r syncwire.Revision) error {
	m, _, key, e := c.Store.unlock(c.Master, r)
	vc.Wipe(key[:])
	if e != nil {
		return e
	}
	origin, e := c.Store.Origin(r.ID)
	if e != nil {
		return e
	}
	c.report(syncwire.Progress{Phase: "downloading", Revision: r.ID, Name: m.Name, Direction: "download", Source: origin, TotalBytes: m.Size, Chunks: m.ChunkCount})
	e = c.Store.applyTracked(context.WithValue(ctx, hydrateKey{}, true), c.Master, syncwire.Change{Sequence: 1, Revision: r}, "", func(done, index uint64, reused bool) {
		c.updateProgress(func(p *syncwire.Progress) {
			p.DoneBytes = done
			p.Chunk = index
			if reused {
				p.ReusedBytes += min(uint64(vc.ChunkSize), m.Size-(index-1)*vc.ChunkSize)
			}
		})
	}, func(ctx context.Context, file string, index uint64) ([]byte, error) {
		return c.request(ctx, "GET", "/v1/blobs/"+file+"/chunks/"+strconv.FormatUint(index, 10), nil, vc.ChunkSize+87)
	})
	if e != nil {
		return e
	}
	c.phase("complete")
	return nil
}

// EnsureContent is an explicit recovery request. It never changes the user's
// background Trash preference, and it leaves the object in Trash on failure.
func (c *Client) EnsureContent(ctx context.Context, revision string) error {
	c.Store.syncMu.Lock()
	defer c.Store.syncMu.Unlock()
	have, e := c.Store.ContentAvailable(c.Master, revision)
	if e != nil {
		return e
	}
	if have {
		return c.Store.ExportContext(ctx, c.Master, revision, io.Discard)
	}
	if e = c.syncDeletions(ctx); e != nil {
		return e
	}
	if e = c.prepareRelay(ctx, false); e != nil {
		return e
	}
	if _, e = c.Store.db.Exec(`UPDATE deferred_content SET wanted=1,explicit=1 WHERE revision=?`, revision); e != nil {
		return ErrStore
	}
	if e = c.contentAction(ctx, "request", []string{revision}); e != nil {
		return e
	}
	states, e := c.contentStates(ctx, []string{revision})
	if e != nil {
		return e
	}
	if !states[revision].Available {
		return ErrContentUnavailable
	}
	r, e := c.Store.localRevision(revision)
	if e != nil {
		return e
	}
	if e = c.fetchContent(ctx, r); e != nil {
		return e
	}
	return c.contentAction(ctx, "received", []string{r.ID})
}

func (c *Client) advanceContentCursor() error {
	cursor, e := c.Store.Cursor()
	if e != nil {
		return e
	}
	rows, e := c.Store.db.Query(`SELECT mr.sequence,EXISTS(SELECT 1 FROM deferred_content d WHERE d.revision=mr.revision AND d.wanted=1) FROM metadata_receipts mr WHERE mr.sequence>? ORDER BY mr.sequence`, cursor)
	if e != nil {
		return ErrStore
	}
	for rows.Next() {
		var seq int64
		var missing bool
		if rows.Scan(&seq, &missing) != nil {
			rows.Close()
			return ErrStore
		}
		if missing {
			break
		}
		cursor = seq
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	_, e = c.Store.db.Exec(`UPDATE local_state SET cursor=MAX(cursor,?) WHERE singleton=1`, cursor)
	return e
}

func recordMetadataReceipt(tx *sql.Tx, change syncwire.Change) error {
	if _, e := vc.ParseID(change.Revision.ID); e != nil {
		return ErrInvalid
	}
	if _, e := tx.Exec(`INSERT INTO metadata_receipts VALUES(?,?) ON CONFLICT(revision) DO UPDATE SET sequence=excluded.sequence`, change.Revision.ID, change.Sequence); e != nil {
		return ErrStore
	}
	if _, e := tx.Exec(`UPDATE content_policy SET metadata_cursor=? WHERE id=1`, change.Sequence); e != nil {
		return ErrStore
	}
	return nil
}
