package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"klefki/core/syncwire"
)

// Existing M2 records are adopted once, without rewriting their ciphertext.
func migrateSync(db *sql.DB) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`CREATE TABLE IF NOT EXISTS sync_schema(version INTEGER PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS changes(sequence INTEGER PRIMARY KEY AUTOINCREMENT, account TEXT NOT NULL REFERENCES accounts(id),revision TEXT NOT NULL,object TEXT NOT NULL,base TEXT NOT NULL,metadata BLOB NOT NULL,file_key BLOB,UNIQUE(account,revision));
 CREATE INDEX IF NOT EXISTS changes_account_sequence ON changes(account,sequence);
 CREATE TABLE IF NOT EXISTS cursors(account TEXT NOT NULL REFERENCES accounts(id),device TEXT NOT NULL,sequence INTEGER NOT NULL,PRIMARY KEY(account,device));
 INSERT INTO changes(account,revision,object,base,metadata,file_key)
 SELECT account,id,id,'',metadata,file_key FROM metadata WHERE NOT EXISTS(SELECT 1 FROM sync_schema WHERE version=1);
 INSERT OR IGNORE INTO sync_schema VALUES(1);`)
	if e != nil {
		return e
	}
	return tx.Commit()
}

var errRevisionConflict = errors.New("revision conflict")

func validRevision(r syncwire.Revision, account string) bool {
	return validID(r.ID) && validID(r.Object) && (r.Base == "" || validID(r.Base)) && r.Base != r.ID && envelope(r.Metadata, 4, account, r.Object) && (len(r.FileKey) == 0 || (len(r.FileKey) == 106 && validID(hex.EncodeToString(r.FileKey[18:34])) && envelope(r.FileKey, 3, account, hex.EncodeToString(r.FileKey[18:34]))))
}
func appendChange(ctx context.Context, tx *sql.Tx, account string, r syncwire.Revision) (int64, bool, error) {
	var old syncwire.Revision
	var sequence int64
	e := tx.QueryRowContext(ctx, `SELECT sequence,object,base,metadata,file_key FROM changes WHERE account=? AND revision=?`, account, r.ID).Scan(&sequence, &old.Object, &old.Base, &old.Metadata, &old.FileKey)
	if e == nil {
		if len(old.Metadata) == 0 {
			var digest []byte
			if e = tx.QueryRowContext(ctx, `SELECT digest FROM relay_revisions WHERE account=? AND revision=?`, account, r.ID).Scan(&digest); e != nil {
				return 0, false, e
			}
			if !bytes.Equal(digest, revisionDigest(r)) {
				return 0, false, errRevisionConflict
			}
			_, e = tx.ExecContext(ctx, `UPDATE changes SET metadata=?,file_key=? WHERE account=? AND revision=?`, r.Metadata, r.FileKey, account, r.ID)
			return sequence, false, e
		}
		if old.Object != r.Object || old.Base != r.Base || !bytes.Equal(old.Metadata, r.Metadata) || !bytes.Equal(old.FileKey, r.FileKey) {
			return 0, false, errRevisionConflict
		}
		return sequence, false, nil
	}
	if e != sql.ErrNoRows {
		return 0, false, e
	}
	if r.Base != "" {
		var object string
		e = tx.QueryRowContext(ctx, `SELECT object FROM changes WHERE account=? AND revision=?`, account, r.Base).Scan(&object)
		if e == sql.ErrNoRows || e == nil && object != r.Object {
			return 0, false, errRevisionConflict
		}
		if e != nil {
			return 0, false, e
		}
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO changes(account,revision,object,base,metadata,file_key) VALUES(?,?,?,?,?,?)`, account, r.ID, r.Object, r.Base, r.Metadata, r.FileKey)
	if e != nil {
		return 0, false, e
	}
	sequence, e = result.LastInsertId()
	if e == nil {
		_, e = tx.ExecContext(ctx, `INSERT INTO relay_revisions VALUES(?,?,?,?)`, account, r.ID, revisionDigest(r), revisionFile(r))
	}
	return sequence, true, e
}
func (s *Server) putRevision(w http.ResponseWriter, r *http.Request) {
	var rev syncwire.Revision
	if !decode(w, r, 1500000, &rev) {
		return
	}
	account := who(r).account
	origin, publisher := r.Header.Get("X-Klefki-Origin-Device"), r.Header.Get("X-Klefki-Device")
	if origin != "" && !validID(origin) {
		fail(w, 400)
		return
	}
	if rev.ID != chi.URLParam(r, "revision") || !validRevision(rev, account) {
		fail(w, 400)
		return
	}
	if s.objectDeleted(account, rev.Object) {
		if file := revisionFile(rev); file != "" {
			if _, e := s.db.Exec(`INSERT OR IGNORE INTO deletion_gc VALUES(?,?)`, account, file); e != nil {
				fail(w, 500)
				return
			}
			if e := s.cleanupDeletions(account); e != nil {
				fail(w, 500)
				return
			}
		}
		fail(w, 410)
		return
	}
	if e := s.relayRevisionSpace(account, rev); e != nil {
		if errors.Is(e, errRelayFull) {
			fail(w, 507)
		} else {
			fail(w, 500)
		}
		return
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500)
		return
	}
	defer tx.Rollback()
	seq, created, e := appendChange(r.Context(), tx, account, rev)
	if errors.Is(e, errRevisionConflict) {
		fail(w, 409)
		return
	}
	if e != nil {
		fail(w, 500)
		return
	}
	if validID(publisher) {
		var registered int
		if e = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM relay_devices WHERE account=? AND device=?`, account, publisher).Scan(&registered); e != nil {
			fail(w, 500)
			return
		}
		if registered == 1 {
			if _, e = tx.ExecContext(r.Context(), `INSERT INTO sync_sources VALUES(?,?,?,?) ON CONFLICT(account,revision) DO UPDATE SET publisher=excluded.publisher`, account, rev.ID, origin, publisher); e != nil {
				fail(w, 500)
				return
			}
		}
	}
	if tx.Commit() != nil {
		fail(w, 500)
		return
	}
	if file := revisionFile(rev); file != "" {
		if _, e = s.db.Exec(`DELETE FROM relay_uploads WHERE account=? AND file=?`, account, file); e != nil {
			fail(w, 500)
			return
		}
	}
	status := 200
	if created {
		status = 201
	}
	reply(w, status, syncwire.Cursor{Sequence: seq})
}
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	after, e := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if e != nil || after < 0 {
		fail(w, 400)
		return
	}
	var maxSeq int64
	e = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(sequence),0) FROM changes WHERE account=?`, who(r).account).Scan(&maxSeq)
	if e != nil {
		fail(w, 500)
		return
	}
	if after > maxSeq {
		fail(w, 409)
		return
	}
	rows, e := s.db.QueryContext(r.Context(), `SELECT sequence,revision,object,base,metadata,file_key FROM changes WHERE account=? AND sequence>? ORDER BY sequence LIMIT ?`, who(r).account, after, syncwire.PageSize)
	if e != nil {
		fail(w, 500)
		return
	}
	defer rows.Close()
	out := syncwire.Page{Changes: []syncwire.Change{}}
	for rows.Next() {
		var c syncwire.Change
		if rows.Scan(&c.Sequence, &c.Revision.ID, &c.Revision.Object, &c.Revision.Base, &c.Revision.Metadata, &c.Revision.FileKey) != nil {
			fail(w, 500)
			return
		}
		if len(c.Revision.Metadata) == 0 {
			c.Revision.Metadata = []byte{}
		}
		out.Changes = append(out.Changes, c)
	}
	if rows.Err() != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}
func (s *Server) getCursor(w http.ResponseWriter, r *http.Request) {
	var c syncwire.Cursor
	e := s.db.QueryRowContext(r.Context(), `SELECT sequence FROM cursors WHERE account=? AND device=?`, who(r).account, who(r).device).Scan(&c.Sequence)
	if e != nil && e != sql.ErrNoRows {
		fail(w, 500)
		return
	}
	reply(w, 200, c)
}
func (s *Server) putCursor(w http.ResponseWriter, r *http.Request) {
	var c syncwire.Cursor
	if !decode(w, r, 256, &c) {
		return
	}
	if c.Sequence < 0 {
		fail(w, 400)
		return
	}
	var latest int64
	e := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(sequence),0) FROM changes WHERE account=?`, who(r).account).Scan(&latest)
	if e != nil {
		fail(w, 500)
		return
	}
	if c.Sequence > latest {
		fail(w, 400)
		return
	}
	_, e = s.db.ExecContext(r.Context(), `INSERT INTO cursors VALUES(?,?,?) ON CONFLICT(account,device) DO UPDATE SET sequence=MAX(sequence,excluded.sequence)`, who(r).account, who(r).device, c.Sequence)
	if e != nil {
		fail(w, 500)
		return
	}
	if device := r.Header.Get("X-Klefki-Device"); validID(device) {
		if _, e = s.db.Exec(`UPDATE relay_devices SET sequence=MAX(sequence,?) WHERE account=? AND device=?`, c.Sequence, who(r).account, device); e != nil {
			fail(w, 500)
			return
		}
		if e = s.cleanupRelay(who(r).account); e != nil {
			fail(w, 500)
			return
		}
	}
	w.WriteHeader(204)
}
