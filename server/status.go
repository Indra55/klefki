package server

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

func migrateStatus(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS sync_device_info(account TEXT NOT NULL,device TEXT NOT NULL,kind TEXT NOT NULL,last_seen INTEGER NOT NULL,PRIMARY KEY(account,device));
 CREATE TABLE IF NOT EXISTS sync_sources(account TEXT NOT NULL,revision TEXT NOT NULL,origin TEXT NOT NULL,publisher TEXT NOT NULL,PRIMARY KEY(account,revision));`)
	return err
}

func (s *Server) presence(w http.ResponseWriter, r *http.Request) {
	var in syncwire.Presence
	if !decode(w, r, 2048, &in) {
		return
	}
	if !validID(in.Device) || in.Device != r.Header.Get("X-Klefki-Device") ||
		(in.ClientKind != "desktop" && in.ClientKind != "web" && in.ClientKind != "pwa" && in.ClientKind != "cli") ||
		(in.Revision != "" && !validID(in.Revision)) ||
		(in.Direction != "" && in.Direction != "upload" && in.Direction != "download") ||
		in.DoneBytes > in.TotalBytes || in.TotalBytes > vc.MaxFileSize {
		fail(w, 400)
		return
	}
	switch in.Phase {
	case "idle", "connecting", "checking", "verifying", "uploading", "downloading", "committing", "acknowledging", "retrying", "waiting", "error", "locked":
	default:
		fail(w, 400)
		return
	}
	account := who(r).account
	var registered int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM relay_devices WHERE account=? AND device=?`, account, in.Device).Scan(&registered); err != nil {
		fail(w, 500)
		return
	}
	if registered != 1 {
		fail(w, 410)
		return
	}
	now := s.config.Now().UnixMilli()
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO sync_device_info VALUES(?,?,?,?) ON CONFLICT(account,device) DO UPDATE SET kind=excluded.kind,last_seen=excluded.last_seen`, account, in.Device, in.ClientKind, now); err != nil {
		fail(w, 500)
		return
	}
	s.presenceMu.Lock()
	for key, value := range s.presences {
		if now-value.LastSeen > int64(5*time.Minute/time.Millisecond) {
			delete(s.presences, key)
		}
	}
	if len(s.presences) < 10000 || s.presences[account+":"+in.Device].LastSeen != 0 {
		s.presences[account+":"+in.Device] = syncwire.DeviceStatus{Presence: in, LastSeen: now}
	}
	s.presenceMu.Unlock()
	w.WriteHeader(204)
}

func (s *Server) syncStatus(w http.ResponseWriter, r *http.Request) {
	account := who(r).account
	out := syncwire.Status{Devices: []syncwire.DeviceStatus{}}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500)
		return
	}
	defer tx.Rollback()
	if tx.QueryRowContext(r.Context(), `SELECT epoch FROM relay_epoch WHERE id=1`).Scan(&out.Epoch) != nil ||
		tx.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(sequence),0) FROM changes WHERE account=?`, account).Scan(&out.LatestSequence) != nil {
		fail(w, 500)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT d.device,d.sequence,COALESCE(i.kind,''),COALESCE(i.last_seen,0),
 (SELECT COUNT(*) FROM changes c WHERE c.account=d.account AND c.sequence>d.sequence)
 FROM relay_devices d LEFT JOIN sync_device_info i ON i.account=d.account AND i.device=d.device WHERE d.account=? ORDER BY d.device`, account)
	if err != nil {
		fail(w, 500)
		return
	}
	for rows.Next() {
		var d syncwire.DeviceStatus
		if rows.Scan(&d.Device, &d.Sequence, &d.ClientKind, &d.LastSeen, &d.Outstanding) != nil {
			rows.Close()
			fail(w, 500)
			return
		}
		s.presenceMu.Lock()
		live, ok := s.presences[account+":"+d.Device]
		s.presenceMu.Unlock()
		// A restart clears live presence even when the last contact remains on disk.
		if ok && s.config.Now().UnixMilli()-live.LastSeen < 45000 {
			d.Presence = live.Presence
		} else {
			d.Phase = "unknown"
		}
		out.Devices = append(out.Devices, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(w, 500)
		return
	}
	if tx.Commit() != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}

func (s *Server) provenance(w http.ResponseWriter, r *http.Request) {
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if err != nil || after < 0 {
		fail(w, 400)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT c.sequence,c.revision,COALESCE(p.origin,''),COALESCE(p.publisher,'') FROM changes c
 LEFT JOIN sync_sources p ON p.account=c.account AND p.revision=c.revision WHERE c.account=? AND c.sequence>? ORDER BY c.sequence LIMIT ?`, who(r).account, after, syncwire.PageSize)
	if err != nil {
		fail(w, 500)
		return
	}
	defer rows.Close()
	out := syncwire.ProvenancePage{Changes: []syncwire.Provenance{}}
	for rows.Next() {
		var p syncwire.Provenance
		if rows.Scan(&p.Sequence, &p.Revision, &p.Origin, &p.Publisher) != nil {
			fail(w, 500)
			return
		}
		out.Changes = append(out.Changes, p)
	}
	if rows.Err() != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}
