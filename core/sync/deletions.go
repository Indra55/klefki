package sync

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

var ErrDeletionUnsupported = errors.New("DELETION_UPDATE_REQUIRED")

func (s *Store) migrateDeletions() error {
	_, e := s.db.Exec(`CREATE TABLE IF NOT EXISTS deletions(object TEXT PRIMARY KEY,folder INTEGER NOT NULL,files TEXT NOT NULL,pending INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS deletion_state(id INTEGER PRIMARY KEY CHECK(id=1),epoch TEXT NOT NULL,cursor INTEGER NOT NULL,supported INTEGER NOT NULL,compact INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO deletion_state(id,epoch,cursor,supported) VALUES(1,'',0,0);`)
	return e
}

type rowQuery interface{ QueryRow(string, ...any) *sql.Row }

func deletedObject(q rowQuery, object string) (bool, error) {
	var n int
	e := q.QueryRow(`SELECT COUNT(*) FROM deletions WHERE object=?`, object).Scan(&n)
	return n != 0, e
}
func (s *Store) Deleted(object string) (bool, error) { return deletedObject(s.db, object) }
func (s *Store) DeletionSupported() bool {
	var supported int
	return s.db.QueryRow(`SELECT supported FROM deletion_state WHERE id=1`).Scan(&supported) == nil && supported == 1
}
func (s *Store) Bound() bool {
	var remote string
	return s.db.QueryRow(`SELECT remote FROM local_state WHERE singleton=1`).Scan(&remote) == nil && remote != ""
}
func fileID(key []byte) string {
	if len(key) != 106 {
		return ""
	}
	return hex.EncodeToString(key[18:34])
}

func deletionBatches(records []syncwire.Deletion) [][]syncwire.Deletion {
	out := [][]syncwire.Deletion{}
	batch := []syncwire.Deletion{}
	size := 32
	for _, record := range records {
		files := record.Files
		for first := true; first || len(files) > 0; first = false {
			d := record
			count := min(len(files), 4096)
			d.Files = files[:count]
			files = files[count:]
			data, _ := json.Marshal(d)
			if len(batch) == 256 || size+len(data) > 1400000 {
				out = append(out, batch)
				batch = []syncwire.Deletion{}
				size = 32
			}
			batch = append(batch, d)
			size += len(data) + 1
		}
	}
	if len(batch) > 0 {
		out = append(out, batch)
	}
	return out
}
func (s *Store) deletionRecords(pending bool) ([]syncwire.Deletion, error) {
	query := `SELECT object,folder,files FROM deletions ORDER BY object`
	if pending {
		query = `SELECT object,folder,files FROM deletions WHERE pending=1 ORDER BY object`
	}
	rows, e := s.db.Query(query)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []syncwire.Deletion{}
	for rows.Next() {
		var d syncwire.Deletion
		var files string
		if e = rows.Scan(&d.Object, &d.Folder, &files); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(files), &d.Files); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Erase records deletion intent and removes all local versions in one transaction.
func (s *Store) Erase(in []syncwire.Deletion, pending bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eraseLocked(in, pending)
}
func (s *Store) eraseLocked(in []syncwire.Deletion, pending bool) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	files := map[string]bool{}
	for _, d := range in {
		if _, e = parseID(d.Object); e != nil {
			return e
		}
		if pending && len(d.Heads) > 0 {
			rows, e := tx.Query(`SELECT id FROM revisions r WHERE object=? AND NOT EXISTS(SELECT 1 FROM revisions child WHERE child.base=r.id) ORDER BY id`, d.Object)
			if e != nil {
				return e
			}
			heads := []string{}
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return e
				}
				heads = append(heads, id)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if !slices.Equal(heads, d.Heads) {
				return ErrConflict
			}
		}
		for _, f := range d.Files {
			if _, e = parseID(f); e != nil {
				return e
			}
			files[f] = true
		}
		rows, e := tx.Query(`SELECT file_key FROM revisions WHERE object=?`, d.Object)
		if e != nil {
			return e
		}
		for rows.Next() {
			var key []byte
			if e = rows.Scan(&key); e != nil {
				rows.Close()
				return e
			}
			if f := fileID(key); f != "" {
				files[f] = true
				d.Files = append(d.Files, f)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		var oldFiles string
		if e = tx.QueryRow(`SELECT files FROM deletions WHERE object=?`, d.Object).Scan(&oldFiles); e == nil {
			var old []string
			if json.Unmarshal([]byte(oldFiles), &old) != nil {
				return ErrStore
			}
			d.Files = append(d.Files, old...)
		} else if e != sql.ErrNoRows {
			return e
		}
		slices.Sort(d.Files)
		d.Files = slices.Compact(d.Files)
		if d.Files == nil {
			d.Files = []string{}
		}
		b, _ := json.Marshal(d.Files)
		if _, e = tx.Exec(`INSERT INTO deletions VALUES(?,?,?,?) ON CONFLICT(object) DO UPDATE SET folder=MAX(folder,excluded.folder),files=excluded.files,pending=MAX(pending,excluded.pending)`, d.Object, d.Folder, string(b), pending); e != nil {
			return e
		}
		if _, e = tx.Exec(`DELETE FROM revision_sources WHERE revision IN(SELECT id FROM revisions WHERE object=?)`, d.Object); e != nil {
			return e
		}
		if _, e = tx.Exec(`UPDATE deletion_state SET compact=1 WHERE EXISTS(SELECT 1 FROM revisions WHERE object=?)`, d.Object); e != nil {
			return e
		}
		if _, e = tx.Exec(`DELETE FROM revisions WHERE object=?`, d.Object); e != nil {
			return e
		}
	}
	rows, e := tx.Query(`SELECT file_key FROM revisions WHERE file_key IS NOT NULL`)
	if e != nil {
		return e
	}
	for rows.Next() {
		var key []byte
		if e = rows.Scan(&key); e != nil {
			rows.Close()
			return e
		}
		delete(files, fileID(key))
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for f := range files {
		if _, e = tx.Exec(`DELETE FROM chunks WHERE file=?`, f); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) advanceDeleted(sequence int64) error {
	_, e := s.db.Exec(`UPDATE local_state SET cursor=MAX(cursor,?) WHERE singleton=1`, sequence)
	return e
}
func (s *Store) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); e != nil {
		return e
	}
	if _, e := s.db.Exec(`VACUUM`); e != nil {
		return e
	}
	if _, e := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); e != nil {
		return e
	}
	_, e := s.db.Exec(`UPDATE deletion_state SET compact=0 WHERE id=1`)
	return e
}

// Reconcile the durable deletion ledger before offering any retained history.
func (c *Client) syncDeletions(ctx context.Context) error {
	var epoch string
	var cursor int64
	if e := c.Store.db.QueryRow(`SELECT epoch,cursor FROM deletion_state WHERE id=1`).Scan(&epoch, &cursor); e != nil {
		return e
	}
	pull := func(after int64) (syncwire.DeletionPage, error) {
		var page syncwire.DeletionPage
		b, e := c.request(ctx, "GET", "/v1/sync/deletions?after="+strconv.FormatInt(after, 10), nil, syncwire.MaxPageBytes)
		if e == nil {
			e = parseJSON(b, &page)
		}
		return page, e
	}
	page, e := pull(cursor)
	if StatusUnsupported(e) {
		_, _ = c.Store.db.Exec(`UPDATE deletion_state SET supported=0 WHERE id=1`)
		if n, err := c.Store.deletionRecords(true); err != nil {
			return err
		} else if len(n) > 0 {
			return ErrDeletionUnsupported
		}
		return nil
	}
	if e != nil {
		return e
	}
	if _, e = parseID(page.Epoch); e != nil {
		return e
	}
	if epoch != page.Epoch {
		cursor = 0
		if _, e = c.Store.db.Exec(`UPDATE deletions SET pending=1`); e != nil {
			return e
		}
		page, e = pull(0)
		if e != nil {
			return e
		}
	}
	for {
		if len(page.Changes) > syncwire.PageSize || page.Latest < cursor {
			return ErrInvalid
		}
		for _, change := range page.Changes {
			if change.Sequence <= cursor || change.Sequence > page.Latest {
				return ErrInvalid
			}
			// Newly learned file IDs must be relayed after a server reset as well.
			if e = c.Store.Erase([]syncwire.Deletion{change.Deletion}, false); e != nil {
				return e
			}
			cursor = change.Sequence
			if _, e = c.Store.db.Exec(`UPDATE deletion_state SET epoch=?,cursor=?,supported=1 WHERE id=1`, page.Epoch, cursor); e != nil {
				return e
			}
		}
		if len(page.Changes) < syncwire.PageSize {
			break
		}
		page, e = pull(cursor)
		if e != nil {
			return e
		}
	}
	if _, e = c.Store.db.Exec(`UPDATE deletion_state SET epoch=?,cursor=?,supported=1 WHERE id=1`, page.Epoch, cursor); e != nil {
		return e
	}
	if e = c.Store.PruneDeleted(c.Master); e != nil {
		return e
	}
	pending, e := c.Store.deletionRecords(true)
	if e != nil {
		return e
	}
	for _, batch := range deletionBatches(pending) {
		if _, e = c.request(ctx, "POST", "/v1/sync/deletions", syncwire.DeletionBatch{Deletions: batch}, 1024); e != nil {
			return e
		}
	}
	for _, d := range pending {
		// Concurrent local additions of file IDs must remain pending.
		b, _ := json.Marshal(d.Files)
		if _, e = c.Store.db.Exec(`UPDATE deletions SET pending=0 WHERE object=? AND files=?`, d.Object, string(b)); e != nil {
			return e
		}
	}
	var compact int
	if e = c.Store.db.QueryRow(`SELECT compact FROM deletion_state WHERE id=1`).Scan(&compact); e != nil {
		return e
	}
	if compact == 1 {
		return c.Store.Compact()
	}
	return nil
}

func internalMetadata(m vc.Metadata) bool {
	for _, t := range m.Tags {
		if t == "klefki:system:search-index" || t == "klefki:system:semantic-index" || t == "klefki:system:analysis-correction" {
			return true
		}
	}
	return false
}
func referencesDeleted(value any, deleted map[string]bool) bool {
	switch v := value.(type) {
	case string:
		return deleted[v]
	case []any:
		for _, x := range v {
			if referencesDeleted(x, deleted) {
				return true
			}
		}
	case map[string]any:
		for _, x := range v {
			if referencesDeleted(x, deleted) {
				return true
			}
		}
	}
	return false
}

// Prune sidecars and descendants discovered after their parent was deleted.
func (s *Store) PruneDeleted(master vc.Key) error {
	defer vc.Wipe(master[:])
	records, e := s.deletionRecords(false)
	if e != nil {
		return e
	}
	if len(records) == 0 {
		return nil
	}
	deleted := map[string]bool{}
	for _, d := range records {
		deleted[d.Object] = true
		for _, f := range d.Files {
			deleted[f] = true
		}
	}
	rows, e := s.db.Query(`SELECT id,object,base,metadata,file_key FROM revisions ORDER BY ordinal`)
	if e != nil {
		return e
	}
	all := []syncwire.Revision{}
	for rows.Next() {
		var r syncwire.Revision
		if e = rows.Scan(&r.ID, &r.Object, &r.Base, &r.Metadata, &r.FileKey); e != nil {
			rows.Close()
			return e
		}
		all = append(all, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	parents := map[string]bool{}
	for _, r := range all {
		parents[r.Base] = true
	}
	targets := map[string]syncwire.Deletion{}
	for changed := true; changed; {
		changed = false
		for _, r := range all {
			if deleted[r.Object] {
				continue
			}
			m, _, key, e := s.unlock(master, r)
			vc.Wipe(key[:])
			if e != nil {
				return e
			}
			remove := !parents[r.ID] && m.Parent != nil && deleted[*m.Parent]
			available, err := s.ContentAvailable(master, r.ID)
			if err != nil {
				return err
			}
			if internalMetadata(m) && !available {
				remove = true
			}
			if internalMetadata(m) && available {
				var data bytes.Buffer
				if m.Size > 64<<20 {
					return ErrInvalid
				}
				if e = s.Export(master, r.ID, &data); e != nil {
					return e
				}
				var value any
				e = json.Unmarshal(data.Bytes(), &value)
				vc.Wipe(data.Bytes())
				remove = remove || e != nil || referencesDeleted(value, deleted)
			}
			if remove {
				deleted[r.Object] = true
				if m.FileID != "" {
					deleted[m.FileID] = true
				}
				targets[r.Object] = syncwire.Deletion{Object: r.Object, Folder: m.Type == "folder", Files: []string{}}
				changed = true
			}
		}
	}
	out := []syncwire.Deletion{}
	for _, d := range targets {
		out = append(out, d)
	}
	if len(out) > 0 {
		return s.Erase(out, true)
	}
	return nil
}
