package server

import (
	"database/sql"
	"net/http"

	"klefki/core/syncwire"
)

func migrateContent(db *sql.DB) error {
	_, e := db.Exec(`CREATE TABLE IF NOT EXISTS content_schema(version INTEGER PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS trash_policy(account TEXT PRIMARY KEY,enabled INTEGER NOT NULL DEFAULT 0,version INTEGER NOT NULL DEFAULT 1,active INTEGER NOT NULL DEFAULT 0,content_version INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS content_notifications(account TEXT NOT NULL,device TEXT NOT NULL,version INTEGER NOT NULL,PRIMARY KEY(account,device));
 CREATE TABLE IF NOT EXISTS content_available(account TEXT NOT NULL,file TEXT NOT NULL,PRIMARY KEY(account,file));
 CREATE TABLE IF NOT EXISTS content_requests(account TEXT NOT NULL,device TEXT NOT NULL,revision TEXT NOT NULL,file TEXT NOT NULL,received INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(account,device,revision));`)
	if e != nil {
		return e
	}
	var migrated bool
	if e = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM content_schema WHERE version=1)`).Scan(&migrated); e != nil || migrated {
		return e
	}
	rows, e := db.Query(`SELECT account,revision,object,base,metadata,file_key FROM changes WHERE length(metadata)>0`)
	if e != nil {
		return e
	}
	type record struct {
		account  string
		revision syncwire.Revision
	}
	records := []record{}
	for rows.Next() {
		var x record
		if e = rows.Scan(&x.account, &x.revision.ID, &x.revision.Object, &x.revision.Base, &x.revision.Metadata, &x.revision.FileKey); e != nil {
			rows.Close()
			return e
		}
		records = append(records, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, x := range records {
		file := revisionFile(x.revision)
		if _, e = tx.Exec(`INSERT OR IGNORE INTO relay_revisions VALUES(?,?,?,?)`, x.account, x.revision.ID, revisionDigest(x.revision), file); e != nil {
			return e
		}
		if file != "" {
			if _, e = tx.Exec(`INSERT OR IGNORE INTO content_available VALUES(?,?)`, x.account, file); e != nil {
				return e
			}
		}
	}
	if _, e = tx.Exec(`INSERT INTO content_schema VALUES(1)`); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Server) contentAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Klefki-Content") == "1" {
			next.ServeHTTP(w, r)
			return
		}
		var active bool
		e := s.db.QueryRow(`SELECT active FROM trash_policy WHERE account=?`, who(r).account).Scan(&active)
		if e != nil && e != sql.ErrNoRows {
			fail(w, 500)
			return
		}
		if active {
			fail(w, 428)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) activateContent(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Klefki-Content") != "1" {
		fail(w, 428)
		return
	}
	_, e := s.db.Exec(`INSERT INTO trash_policy(account,active) VALUES(?,1) ON CONFLICT(account) DO UPDATE SET active=1`, who(r).account)
	if e != nil {
		fail(w, 500)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) putTrashPolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if !decode(w, r, 256, &in) {
		return
	}
	if in.Enabled == nil {
		fail(w, 400)
		return
	}
	_, e := s.db.Exec(`INSERT INTO trash_policy(account,enabled) VALUES(?,?) ON CONFLICT(account) DO UPDATE SET enabled=excluded.enabled,version=version+1`, who(r).account, *in.Enabled)
	if e != nil {
		fail(w, 500)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) contentDevice(r *http.Request) (string, bool) {
	device := r.Header.Get("X-Klefki-Device")
	var n int
	return device, validID(device) && s.db.QueryRow(`SELECT COUNT(*) FROM relay_devices WHERE account=? AND device=?`, who(r).account, device).Scan(&n) == nil && n == 1
}

// Requests are registered before the metadata cursor advances. Cleanup may
// discard an envelope, but never a requested file without a durable receipt.
func (s *Server) contentAction(w http.ResponseWriter, r *http.Request) {
	var in syncwire.ContentAction
	if !decode(w, r, 16384, &in) {
		return
	}
	if len(in.IDs) > syncwire.RelayBatchSize || (in.Action != "request" && in.Action != "received" && in.Action != "available" && in.Action != "release") {
		fail(w, 400)
		return
	}
	device, ok := s.contentDevice(r)
	if !ok {
		fail(w, 428)
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		fail(w, 500)
		return
	}
	defer tx.Rollback()
	account := who(r).account
	for _, id := range in.IDs {
		if !validID(id) {
			fail(w, 400)
			return
		}
		var file, object string
		e = tx.QueryRow(`SELECT rr.file,c.object FROM relay_revisions rr JOIN changes c ON c.account=rr.account AND c.revision=rr.revision WHERE rr.account=? AND rr.revision=?`, account, id).Scan(&file, &object)
		if e == sql.ErrNoRows {
			fail(w, 409)
			return
		}
		if e != nil {
			fail(w, 500)
			return
		}
		var deleted int
		if e = tx.QueryRow(`SELECT COUNT(*) FROM deletions WHERE account=? AND object=?`, account, object).Scan(&deleted); e != nil {
			fail(w, 500)
			return
		}
		if deleted > 0 {
			fail(w, 410)
			return
		}
		if file == "" {
			fail(w, 400)
			return
		}
		var result sql.Result
		switch in.Action {
		case "request":
			result, e = tx.Exec(`INSERT INTO content_requests(account,device,revision,file) VALUES(?,?,?,?) ON CONFLICT(account,device,revision) DO UPDATE SET received=0 WHERE received=1`, account, device, id, file)
		case "release":
			if _, e = tx.Exec(`DELETE FROM relay_uploads WHERE account=? AND file=?`, account, file); e != nil {
				fail(w, 500)
				return
			}
			result, e = tx.Exec(`DELETE FROM content_requests WHERE account=? AND device=? AND revision=?`, account, device, id)
		case "received":
			result, e = tx.Exec(`INSERT INTO content_requests(account,device,revision,file,received) VALUES(?,?,?,?,1) ON CONFLICT(account,device,revision) DO UPDATE SET received=1 WHERE received=0`, account, device, id, file)
		case "available":
			result, e = tx.Exec(`INSERT OR IGNORE INTO content_available VALUES(?,?)`, account, file)
			if e == nil {
				_, e = tx.Exec(`DELETE FROM relay_uploads WHERE account=? AND file=?`, account, file)
			}
		}
		if e != nil {
			fail(w, 500)
			return
		}
		if n, _ := result.RowsAffected(); n > 0 {
			if _, e = tx.Exec(`UPDATE trash_policy SET content_version=content_version+1 WHERE account=?`, account); e != nil {
				fail(w, 500)
				return
			}
			if in.Action == "request" || in.Action == "available" {
				if _, e = tx.Exec(`INSERT INTO content_events(account,device,file) VALUES(?,?,?)`, account, device, file); e != nil {
					fail(w, 500)
					return
				}
				if _, e = tx.Exec(`INSERT INTO content_notifications(account,device,version) SELECT d.account,d.device,p.content_version FROM relay_devices d JOIN trash_policy p ON p.account=d.account WHERE d.account=? AND d.device<>? ON CONFLICT(account,device) DO UPDATE SET version=excluded.version`, account, device); e != nil {
					fail(w, 500)
					return
				}
			}
		}
	}
	if tx.Commit() != nil {
		fail(w, 500)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) contentStates(w http.ResponseWriter, r *http.Request) {
	var in syncwire.ContentBatch
	if !decode(w, r, 16384, &in) {
		return
	}
	if len(in.IDs) > syncwire.RelayBatchSize {
		fail(w, 400)
		return
	}
	if _, ok := s.contentDevice(r); !ok {
		fail(w, 428)
		return
	}
	account := who(r).account
	out := syncwire.ContentStates{States: []syncwire.ContentState{}}
	settings, e := s.settings(account)
	if e != nil {
		fail(w, 500)
		return
	}
	var count int
	var floor int64
	if e = s.db.QueryRow(`SELECT COUNT(*),COALESCE(MIN(sequence),0) FROM relay_devices WHERE account=?`, account).Scan(&count, &floor); e != nil {
		fail(w, 500)
		return
	}
	for _, id := range in.IDs {
		if !validID(id) {
			fail(w, 400)
			return
		}
		var file string
		var seq int64
		e = s.db.QueryRow(`SELECT rr.file,c.sequence FROM relay_revisions rr JOIN changes c ON c.account=rr.account AND c.revision=rr.revision WHERE rr.account=? AND rr.revision=?`, account, id).Scan(&file, &seq)
		if e == sql.ErrNoRows {
			fail(w, 409)
			return
		}
		if e != nil {
			fail(w, 500)
			return
		}
		var available, wanted int
		if e = s.db.QueryRow(`SELECT COUNT(*) FROM content_available WHERE account=? AND file=?`, account, file).Scan(&available); e != nil {
			fail(w, 500)
			return
		}
		if e = s.db.QueryRow(`SELECT COUNT(*) FROM content_requests WHERE account=? AND file=? AND received=0`, account, file).Scan(&wanted); e != nil {
			fail(w, 500)
			return
		}
		var missing int
		if e = s.db.QueryRow(`SELECT COUNT(*) FROM relay_devices d WHERE d.account=? AND NOT EXISTS(SELECT 1 FROM content_requests cr WHERE cr.account=d.account AND cr.device=d.device AND cr.file=? AND cr.received=1)`, account, file).Scan(&missing); e != nil {
			fail(w, 500)
			return
		}
		out.States = append(out.States, syncwire.ContentState{ID: id, Available: available > 0, Requested: wanted > 0 || settings.Mode != syncwire.TemporaryRelay || count < 2 || seq > floor && missing > 0})
	}
	reply(w, 200, out)
}
