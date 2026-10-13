package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"klefki/core/syncwire"
)

func migrateDeletions(db *sql.DB) error {
	_, e := db.Exec(`CREATE TABLE IF NOT EXISTS deletions(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account TEXT NOT NULL REFERENCES accounts(id),object TEXT NOT NULL,folder INTEGER NOT NULL,files TEXT NOT NULL,UNIQUE(account,object));
 CREATE TABLE IF NOT EXISTS deleted_files(account TEXT NOT NULL,file TEXT NOT NULL,PRIMARY KEY(account,file));
 CREATE TABLE IF NOT EXISTS deletion_gc(account TEXT NOT NULL,file TEXT NOT NULL,PRIMARY KEY(account,file));`)
	return e
}
func (s *Server) deletionEnabled(account string) bool {
	var n int
	return s.db.QueryRow(`SELECT COUNT(*) FROM deletions WHERE account=?`, account).Scan(&n) == nil && n > 0
}
func (s *Server) deletionAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deletionEnabled(who(r).account) {
			legacy := strings.HasPrefix(r.URL.Path, "/v1/metadata")
			if legacy || r.Header.Get("X-Klefki-Deletion") != "1" {
				fail(w, 428)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) objectDeleted(account, object string) bool {
	var n int
	return s.db.QueryRow(`SELECT COUNT(*) FROM deletions WHERE account=? AND object=?`, account, object).Scan(&n) == nil && n > 0
}
func (s *Server) blobDeleted(account, file string) bool {
	var n int
	return s.db.QueryRow(`SELECT COUNT(*) FROM deleted_files WHERE account=? AND file=?`, account, file).Scan(&n) == nil && n > 0
}
func (s *Server) getDeletions(w http.ResponseWriter, r *http.Request) {
	after, e := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if e != nil || after < 0 {
		fail(w, 400)
		return
	}
	account := who(r).account
	settings, e := s.settings(account)
	if e != nil {
		fail(w, 500)
		return
	}
	out := syncwire.DeletionPage{Epoch: settings.Epoch, Enabled: s.deletionEnabled(account), Changes: []syncwire.DeletionChange{}}
	if e = s.db.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM deletions WHERE account=?`, account).Scan(&out.Latest); e != nil {
		fail(w, 500)
		return
	}
	// A reset may leave the caller's cursor beyond the new ledger; epoch handles it.
	rows, e := s.db.Query(`SELECT sequence,object,folder,files FROM deletions WHERE account=? AND sequence>? ORDER BY sequence LIMIT ?`, account, after, syncwire.PageSize)
	if e != nil {
		fail(w, 500)
		return
	}
	for rows.Next() {
		var d syncwire.DeletionChange
		var files string
		if e = rows.Scan(&d.Sequence, &d.Deletion.Object, &d.Deletion.Folder, &files); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(files), &d.Deletion.Files); e != nil {
			break
		}
		out.Changes = append(out.Changes, d)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		fail(w, 500)
		return
	}
	if e = s.cleanupDeletions(account); e != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}
func (s *Server) putDeletions(w http.ResponseWriter, r *http.Request) {
	var in syncwire.DeletionBatch
	if !decode(w, r, 1500000, &in) {
		return
	}
	if len(in.Deletions) == 0 || len(in.Deletions) > 256 {
		fail(w, 400)
		return
	}
	for _, d := range in.Deletions {
		if !validID(d.Object) || len(d.Files) > 4096 || len(d.Heads) > 256 {
			fail(w, 400)
			return
		}
		for _, id := range append(slices.Clone(d.Files), d.Heads...) {
			if !validID(id) {
				fail(w, 400)
				return
			}
		}
	}
	account := who(r).account
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500)
		return
	}
	defer tx.Rollback()
	for _, d := range in.Deletions {
		var sequence int64
		var old string
		var folder bool
		e = tx.QueryRow(`SELECT sequence,files,folder FROM deletions WHERE account=? AND object=?`, account, d.Object).Scan(&sequence, &old, &folder)
		exists := e == nil
		if e != nil && e != sql.ErrNoRows {
			fail(w, 500)
			return
		}
		if !exists && len(d.Heads) > 0 {
			rows, e := tx.Query(`SELECT revision FROM changes c WHERE account=? AND object=? AND NOT EXISTS(SELECT 1 FROM changes child WHERE child.account=c.account AND child.base=c.revision) ORDER BY revision`, account, d.Object)
			if e != nil {
				fail(w, 500)
				return
			}
			heads := []string{}
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					break
				}
				heads = append(heads, id)
			}
			if e == nil {
				e = rows.Err()
			}
			rows.Close()
			if e != nil {
				fail(w, 500)
				return
			}
			expected := slices.Clone(d.Heads)
			slices.Sort(expected)
			if !slices.Equal(heads, expected) {
				fail(w, 409)
				return
			}
		}
		if exists {
			var ids []string
			if json.Unmarshal([]byte(old), &ids) != nil {
				fail(w, 500)
				return
			}
			d.Files = append(d.Files, ids...)
			d.Folder = d.Folder || folder
		}
		rows, e := tx.Query(`SELECT DISTINCT file FROM relay_revisions rr JOIN changes c ON c.account=rr.account AND c.revision=rr.revision WHERE c.account=? AND c.object=? AND file<>''`, account, d.Object)
		if e != nil {
			fail(w, 500)
			return
		}
		for rows.Next() {
			var file string
			if e = rows.Scan(&file); e != nil {
				break
			}
			d.Files = append(d.Files, file)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			fail(w, 500)
			return
		}
		slices.Sort(d.Files)
		d.Files = slices.Compact(d.Files)
		if d.Files == nil {
			d.Files = []string{}
		}
		data, _ := json.Marshal(d.Files)
		if _, e = tx.Exec(`INSERT INTO deletions(account,object,folder,files) VALUES(?,?,?,?) ON CONFLICT(account,object) DO UPDATE SET folder=excluded.folder,files=excluded.files`, account, d.Object, d.Folder, string(data)); e != nil {
			fail(w, 500)
			return
		}
		// Keep revision identity/edges for existing sync cursors, without payloads.
		if _, e = tx.Exec(`UPDATE changes SET metadata=X'',file_key=NULL WHERE account=? AND object=?`, account, d.Object); e != nil {
			fail(w, 500)
			return
		}
		if _, e = tx.Exec(`DELETE FROM metadata WHERE account=? AND id=?`, account, d.Object); e != nil {
			fail(w, 500)
			return
		}
		for _, file := range d.Files {
			if _, e = tx.Exec(`INSERT OR IGNORE INTO deletion_gc VALUES(?,?)`, account, file); e != nil {
				fail(w, 500)
				return
			}
		}
	}
	if tx.Commit() != nil {
		fail(w, 500)
		return
	}
	if e = s.cleanupDeletions(account); e != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, struct{}{})
}

// The durable queue survives a failure between database commit and unlink.
func (s *Server) cleanupDeletions(account string) error {
	rows, e := s.db.Query(`SELECT file FROM deletion_gc WHERE account=?`, account)
	if e != nil {
		return e
	}
	files := []string{}
	for rows.Next() {
		var f string
		if e = rows.Scan(&f); e != nil {
			break
		}
		files = append(files, f)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	for _, file := range files {
		var references int
		if e = s.db.QueryRow(`SELECT COUNT(*) FROM relay_revisions rr JOIN changes c ON c.account=rr.account AND c.revision=rr.revision WHERE rr.account=? AND rr.file=? AND NOT EXISTS(SELECT 1 FROM deletions d WHERE d.account=c.account AND d.object=c.object)`, account, file).Scan(&references); e != nil {
			return e
		}
		if references > 0 {
			continue
		}
		if _, e = s.db.Exec(`INSERT OR IGNORE INTO deleted_files VALUES(?,?)`, account, file); e != nil {
			return e
		}
		disk, ok := s.blobs.(*DiskStore)
		if !ok {
			return ErrConflict
		}
		dir, e := disk.dir(account, file)
		if e != nil {
			return e
		}
		if e = os.RemoveAll(dir); e != nil {
			return e
		}
		if _, e = s.db.Exec(`DELETE FROM relay_uploads WHERE account=? AND file=?`, account, file); e != nil {
			return e
		}
		if _, e = s.db.Exec(`DELETE FROM deletion_gc WHERE account=? AND file=?`, account, file); e != nil {
			return e
		}
	}
	_, e = s.db.Exec(`PRAGMA incremental_vacuum(256)`)
	return e
}
