package server

import (
	"database/sql"
	"net/http"
	"strconv"

	"klefki/core/syncwire"
)

func migrateWork(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS content_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account TEXT NOT NULL,device TEXT NOT NULL,file TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS content_event_account ON content_events(account,sequence);
 CREATE INDEX IF NOT EXISTS content_request_file ON content_requests(account,file,received,device);
 CREATE TABLE IF NOT EXISTS relay_inventory(account TEXT PRIMARY KEY,version INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS relay_metadata(account TEXT NOT NULL,device TEXT NOT NULL,sequence INTEGER NOT NULL,PRIMARY KEY(account,device));`)
	return err
}

func (s *Server) workCheck(w http.ResponseWriter, r *http.Request) {
	device, ok := s.contentDevice(r)
	if !ok {
		fail(w, 428)
		return
	}
	account := who(r).account
	var out syncwire.WorkCheck
	err := s.db.QueryRowContext(r.Context(), `SELECT
 (SELECT epoch FROM relay_epoch WHERE id=1),
 COALESCE((SELECT mode FROM relay_settings WHERE account=?),'persistent'),
 (SELECT COALESCE(MAX(sequence),0) FROM changes WHERE account=?),
 (SELECT COALESCE(MAX(sequence),0) FROM content_events WHERE account=? AND device<>?),
 (SELECT COALESCE(MAX(sequence),0) FROM deletions WHERE account=?),
 COALESCE((SELECT version FROM trash_policy WHERE account=?),1),
 COALESCE((SELECT version FROM relay_inventory WHERE account=?),0)`, account, account, account, device, account, account, account).Scan(&out.Epoch, &out.Mode, &out.Metadata, &out.Content, &out.Deletion, &out.Policy, &out.Inventory)
	if err != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}

func (s *Server) contentChanges(w http.ResponseWriter, r *http.Request) {
	device, ok := s.contentDevice(r)
	if !ok {
		fail(w, 428)
		return
	}
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if err != nil || after < 0 {
		fail(w, 400)
		return
	}
	account := who(r).account
	var out syncwire.ContentEvents
	out.Changes = []syncwire.ContentEvent{}
	if err = s.db.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM content_events WHERE account=? AND device<>?`, account, device).Scan(&out.Latest); err != nil {
		fail(w, 500)
		return
	}
	if after > out.Latest {
		fail(w, 409)
		return
	}
	through := out.Latest
	if value := r.URL.Query().Get("through"); value != "" {
		through, err = strconv.ParseInt(value, 10, 64)
		if err != nil || through < after || through > out.Latest {
			fail(w, 400)
			return
		}
	}
	out.Latest = through
	rows, err := s.db.QueryContext(r.Context(), `SELECT sequence,file FROM content_events WHERE account=? AND device<>? AND sequence>? AND sequence<=? ORDER BY sequence LIMIT ?`, account, device, after, through, syncwire.RelayBatchSize)
	if err != nil {
		fail(w, 500)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x syncwire.ContentEvent
		if rows.Scan(&x.Sequence, &x.File) != nil {
			fail(w, 500)
			return
		}
		out.Changes = append(out.Changes, x)
	}
	if rows.Err() != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}

func (s *Server) putMetadataCursor(w http.ResponseWriter, r *http.Request) {
	device, ok := s.contentDevice(r)
	if !ok {
		fail(w, 428)
		return
	}
	var in syncwire.Cursor
	if !decode(w, r, 256, &in) {
		return
	}
	var latest int64
	account := who(r).account
	if s.db.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM changes WHERE account=?`, account).Scan(&latest) != nil {
		fail(w, 500)
		return
	}
	if in.Sequence < 0 || in.Sequence > latest {
		fail(w, 400)
		return
	}
	if _, err := s.db.Exec(`INSERT INTO relay_metadata VALUES(?,?,?) ON CONFLICT(account,device) DO UPDATE SET sequence=MAX(sequence,excluded.sequence)`, account, device, in.Sequence); err != nil {
		fail(w, 500)
		return
	}
	if err := s.cleanupRelay(account); err != nil {
		fail(w, 500)
		return
	}
	w.WriteHeader(204)
}
