package server

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

var errRelayFull = errors.New("relay storage limit reached")

func migrateRelay(db *sql.DB) error {
	_, e := db.Exec(`CREATE TABLE IF NOT EXISTS relay_settings(account TEXT PRIMARY KEY REFERENCES accounts(id),mode TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS relay_epoch(id INTEGER PRIMARY KEY CHECK(id=1),epoch TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS relay_devices(account TEXT NOT NULL REFERENCES accounts(id),device TEXT NOT NULL,sequence INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(account,device));
 CREATE TABLE IF NOT EXISTS relay_revisions(account TEXT NOT NULL,revision TEXT NOT NULL,digest BLOB NOT NULL,file TEXT NOT NULL,PRIMARY KEY(account,revision));
 CREATE TABLE IF NOT EXISTS relay_uploads(account TEXT NOT NULL,file TEXT NOT NULL,PRIMARY KEY(account,file));`)
	if e != nil {
		return e
	}
	if _, e = db.Exec(`CREATE TABLE IF NOT EXISTS relay_removed(account TEXT NOT NULL,device TEXT NOT NULL,PRIMARY KEY(account,device))`); e != nil {
		return e
	}
	id, e := vc.RandomID()
	if e != nil {
		return e
	}
	_, e = db.Exec(`INSERT OR IGNORE INTO relay_epoch VALUES(1,?)`, id.String())
	return e
}

func revisionDigest(r syncwire.Revision) []byte {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return h[:]
}
func revisionFile(r syncwire.Revision) string {
	if len(r.FileKey) == 106 {
		return hex.EncodeToString(r.FileKey[18:34])
	}
	return ""
}
func (s *Server) settings(account string) (syncwire.SyncSettings, error) {
	out := syncwire.SyncSettings{Mode: syncwire.Persistent}
	e := s.db.QueryRow(`SELECT mode FROM relay_settings WHERE account=?`, account).Scan(&out.Mode)
	if e != nil && e != sql.ErrNoRows {
		return out, e
	}
	e = s.db.QueryRow(`SELECT epoch FROM relay_epoch WHERE id=1`).Scan(&out.Epoch)
	if e == nil {
		e = s.db.QueryRow(`SELECT COUNT(*) FROM changes WHERE account=? AND length(metadata)>0`, account).Scan(&out.Queued)
	}
	return out, e
}
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	out, e := s.settings(who(r).account)
	if e != nil {
		fail(w, 500)
		return
	}
	if r.Header.Get("X-Klefki-Deletion") == "1" {
		out.DeletionSupported = true
		if e = s.db.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM deletions WHERE account=?`, who(r).account).Scan(&out.DeletionSequence); e != nil {
			fail(w, 500)
			return
		}
	}
	reply(w, 200, out)
}
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string `json:"mode"`
	}
	if !decode(w, r, 256, &in) {
		return
	}
	if in.Mode != syncwire.Persistent && in.Mode != syncwire.TemporaryRelay {
		fail(w, 400)
		return
	}
	account := who(r).account
	// Index existing history before any payload can be discarded.
	rows, e := s.db.Query(`SELECT revision,object,base,metadata,file_key FROM changes WHERE account=? AND length(metadata)>0`, account)
	if e != nil {
		fail(w, 500)
		return
	}
	var revisions []syncwire.Revision
	for rows.Next() {
		var rev syncwire.Revision
		if e = rows.Scan(&rev.ID, &rev.Object, &rev.Base, &rev.Metadata, &rev.FileKey); e != nil {
			break
		}
		revisions = append(revisions, rev)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		fail(w, 500)
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		fail(w, 500)
		return
	}
	defer tx.Rollback()
	for _, rev := range revisions {
		if _, e = tx.Exec(`INSERT OR IGNORE INTO relay_revisions VALUES(?,?,?,?)`, account, rev.ID, revisionDigest(rev), revisionFile(rev)); e != nil {
			fail(w, 500)
			return
		}
	}
	if _, e = tx.Exec(`INSERT INTO relay_settings VALUES(?,?) ON CONFLICT(account) DO UPDATE SET mode=excluded.mode`, account, in.Mode); e != nil {
		fail(w, 500)
		return
	}
	// Legacy metadata contains a second copy of payloads. The change log is now authoritative.
	if in.Mode == syncwire.TemporaryRelay {
		if _, e = tx.Exec(`DELETE FROM metadata WHERE account=?`, account); e != nil {
			fail(w, 500)
			return
		}
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500)
		return
	}
	if in.Mode == syncwire.TemporaryRelay {
		var auto int
		if s.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&auto) != nil {
			fail(w, 500)
			return
		}
		if auto == 0 {
			if _, e = s.db.Exec(`PRAGMA auto_vacuum=INCREMENTAL; VACUUM`); e != nil {
				fail(w, 500)
				return
			}
		}
	}
	out, e := s.settings(account)
	if e != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}

// Serialize content publication and cleanup. Older clients fail closed in relay mode.
func (s *Server) relayAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		settings, e := s.settings(who(r).account)
		if e != nil {
			fail(w, 500)
			return
		}
		control := r.URL.Path == "/v1/sync/settings" || strings.HasPrefix(r.URL.Path, "/v1/sync/relay") || r.URL.Path == "/v1/sync/deletions"
		if settings.Mode == syncwire.TemporaryRelay && !control && (strings.HasPrefix(r.URL.Path, "/v1/sync/") || strings.HasPrefix(r.URL.Path, "/v1/blobs/") || strings.HasPrefix(r.URL.Path, "/v1/metadata")) {
			var n int
			id := r.Header.Get("X-Klefki-Device")
			if !validID(id) || s.db.QueryRow(`SELECT COUNT(*) FROM relay_devices WHERE account=? AND device=?`, who(r).account, id).Scan(&n) != nil || n != 1 {
				fail(w, 428)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/v1/metadata") {
				fail(w, 410)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) relayDevice(w http.ResponseWriter, r *http.Request) {
	var in syncwire.RelayDevice
	if !decode(w, r, 8192, &in) {
		return
	}
	if !validRelayDevice(in) {
		fail(w, 400)
		return
	}
	out, status := s.enrollRelay(who(r).account, in)
	if status != 200 {
		fail(w, status)
		return
	}
	reply(w, 200, out)
}

func (s *Server) enrollRelay(account string, in syncwire.RelayDevice) (out syncwire.RelayState, status int) {
	tx, e := s.db.Begin()
	if e != nil {
		return out, 500
	}
	defer tx.Rollback()
	for _, id := range append(in.Known, in.Device) {
		var removed int
		if tx.QueryRow(`SELECT COUNT(*) FROM relay_removed WHERE account=? AND device=?`, account, id).Scan(&removed) != nil {
			return out, 500
		}
		if removed > 0 {
			if id == in.Device {
				return out, 410
			}
			continue
		}
		if _, e = tx.Exec(`INSERT OR IGNORE INTO relay_devices(account,device) VALUES(?,?)`, account, id); e != nil {
			return out, 500
		}
	}
	// A restored local snapshot can be older than this device's previous receipt.
	if _, e = tx.Exec(`UPDATE relay_devices SET sequence=MIN(sequence,?) WHERE account=? AND device=?`, in.Sequence, account, in.Device); e != nil {
		return out, 500
	}
	var count int
	if tx.QueryRow(`SELECT COUNT(*) FROM relay_devices WHERE account=?`, account).Scan(&count) != nil || count > 64 {
		return out, 409
	}
	if tx.Commit() != nil {
		return out, 500
	}
	settings, e := s.settings(account)
	if e != nil {
		return out, 500
	}
	rows, e := s.db.Query(`SELECT device FROM relay_devices WHERE account=? ORDER BY device`, account)
	if e != nil {
		return out, 500
	}
	defer rows.Close()
	out = syncwire.RelayState{SyncSettings: settings, Devices: []string{}}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return out, 500
		}
		out.Devices = append(out.Devices, id)
	}
	if rows.Err() != nil {
		return out, 500
	}
	return out, 200
}

func (s *Server) relayNeed(w http.ResponseWriter, r *http.Request) {
	var in syncwire.RelayNeed
	if !decode(w, r, 2048, &in) {
		return
	}
	if len(in.IDs) > syncwire.PageSize {
		fail(w, 400)
		return
	}
	out, status := s.neededRelay(who(r).account, in.IDs)
	if status != 200 {
		fail(w, status)
		return
	}
	reply(w, 200, out)
}
func (s *Server) neededRelay(account string, ids []string) (out syncwire.RelayNeed, status int) {
	settings, e := s.settings(account)
	if e != nil {
		return out, 500
	}
	var floor int64
	if s.db.QueryRow(`SELECT COALESCE(MIN(sequence),0) FROM relay_devices WHERE account=?`, account).Scan(&floor) != nil {
		return out, 500
	}
	out = syncwire.RelayNeed{IDs: []string{}}
	for _, id := range ids {
		if !validID(id) {
			return out, 400
		}
		var seq int64
		var size int
		e = s.db.QueryRow(`SELECT sequence,length(metadata) FROM changes WHERE account=? AND revision=?`, account, id).Scan(&seq, &size)
		if e == sql.ErrNoRows || e == nil && size == 0 && (settings.Mode == syncwire.Persistent || seq > floor) {
			out.IDs = append(out.IDs, id)
		} else if e != nil {
			return out, 500
		}
	}
	return out, 200
}

func (s *Server) cleanupRelay(account string) error {
	settings, e := s.settings(account)
	if e != nil || settings.Mode != syncwire.TemporaryRelay {
		return e
	}
	var count int
	var floor int64
	if e = s.db.QueryRow(`SELECT COUNT(*),COALESCE(MIN(sequence),0) FROM relay_devices WHERE account=?`, account).Scan(&count, &floor); e != nil {
		return e
	}
	if count < 2 || floor == 0 {
		return nil
	}
	// Clear encrypted metadata and wrapped keys as well as document chunks.
	result, e := s.db.Exec(`UPDATE changes SET metadata=X'',file_key=NULL WHERE account=? AND sequence<=? AND length(metadata)>0`, account, floor)
	if e != nil {
		return e
	}
	rows, e := s.db.Query(`SELECT DISTINCT file FROM relay_revisions rr WHERE account=? AND file<>'' AND NOT EXISTS
 (SELECT 1 FROM relay_revisions x JOIN changes c ON c.account=x.account AND c.revision=x.revision WHERE x.account=rr.account AND x.file=rr.file AND c.sequence>?)
 AND NOT EXISTS(SELECT 1 FROM relay_uploads u WHERE u.account=rr.account AND u.file=rr.file)`, account, floor)
	if e != nil {
		return e
	}
	var files []string
	for rows.Next() {
		var file string
		if e = rows.Scan(&file); e != nil {
			break
		}
		files = append(files, file)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	disk := s.blobs.(*DiskStore)
	for _, file := range files {
		dir, e := disk.dir(account, file)
		if e != nil {
			return e
		}
		if e = os.RemoveAll(dir); e != nil {
			return e
		}
	}
	if n, _ := result.RowsAffected(); n > 0 {
		if _, e = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE); PRAGMA incremental_vacuum`); e != nil {
			return e
		}
	}
	return nil
}

func (s *Server) forgetRelayDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "device")
	if !validID(id) || id == r.Header.Get("X-Klefki-Device") {
		fail(w, 400)
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		fail(w, 500)
		return
	}
	defer tx.Rollback()
	result, e := tx.Exec(`DELETE FROM relay_devices WHERE account=? AND device=?`, who(r).account, id)
	if e != nil {
		fail(w, 500)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		fail(w, 404)
		return
	}
	if _, e = tx.Exec(`INSERT OR IGNORE INTO relay_removed VALUES(?,?)`, who(r).account, id); e != nil {
		fail(w, 500)
		return
	}
	if tx.Commit() != nil {
		fail(w, 500)
		return
	}
	if e = s.cleanupRelay(who(r).account); e != nil {
		fail(w, 500)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) relayChunk(account, file string, index uint64, b []byte) error {
	settings, e := s.settings(account)
	if e != nil || settings.Mode != syncwire.TemporaryRelay {
		return e
	}
	disk := s.blobs.(*DiskStore)
	dir, e := disk.dir(account, file)
	if e != nil {
		return e
	}
	if old, e := os.ReadFile(filepath.Join(dir, strconv.FormatUint(index, 10))); e == nil && bytes.Equal(old, b) {
		_, e = s.db.Exec(`INSERT OR IGNORE INTO relay_uploads VALUES(?,?)`, account, file)
		return e
	}
	var used int64
	e = filepath.WalkDir(filepath.Join(disk.root, account), func(_ string, d os.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !d.IsDir() {
			info, e := d.Info()
			if e != nil {
				return e
			}
			used += info.Size()
		}
		return nil
	})
	if e != nil {
		return e
	}
	var records int64
	if e = s.db.QueryRow(`SELECT COALESCE(SUM(length(metadata)+COALESCE(length(file_key),0)),0) FROM changes WHERE account=?`, account).Scan(&records); e != nil {
		return e
	}
	used += records
	if used+int64(len(b)) > s.config.RelayMaxBytes {
		return errRelayFull
	}
	_, e = s.db.Exec(`INSERT OR IGNORE INTO relay_uploads VALUES(?,?)`, account, file)
	return e
}

func (s *Server) relayRevisionSpace(account string, rev syncwire.Revision) error {
	settings, e := s.settings(account)
	if e != nil || settings.Mode != syncwire.TemporaryRelay {
		return e
	}
	var old int64
	e = s.db.QueryRow(`SELECT length(metadata)+COALESCE(length(file_key),0) FROM changes WHERE account=? AND revision=?`, account, rev.ID).Scan(&old)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	var used int64
	if e = s.db.QueryRow(`SELECT COALESCE(SUM(length(metadata)+COALESCE(length(file_key),0)),0) FROM changes WHERE account=?`, account).Scan(&used); e != nil {
		return e
	}
	disk := s.blobs.(*DiskStore)
	e = filepath.WalkDir(filepath.Join(disk.root, account), func(_ string, d os.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !d.IsDir() {
			info, e := d.Info()
			if e != nil {
				return e
			}
			used += info.Size()
		}
		return nil
	})
	if e != nil {
		return e
	}
	if used-old+int64(len(rev.Metadata)+len(rev.FileKey)) > s.config.RelayMaxBytes {
		return errRelayFull
	}
	return nil
}

func validRelayDevice(in syncwire.RelayDevice) bool {
	if !validID(in.Device) || len(in.Known) > 64 || in.Sequence < 0 {
		return false
	}
	for _, id := range in.Known {
		if !validID(id) {
			return false
		}
	}
	return true
}
func validRelayIDs(ids []string) bool {
	if len(ids) > syncwire.RelayBatchSize {
		return false
	}
	for _, id := range ids {
		if !validID(id) {
			return false
		}
	}
	return true
}
func (s *Server) relayPrepare(w http.ResponseWriter, r *http.Request) {
	var in syncwire.RelayPrepare
	if !decode(w, r, 16384, &in) {
		return
	}
	if !validRelayDevice(in.RelayDevice) || !validRelayIDs(in.IDs) {
		fail(w, 400)
		return
	}
	state, status := s.enrollRelay(who(r).account, in.RelayDevice)
	if status != 200 {
		fail(w, status)
		return
	}
	needed, status := s.neededRelay(who(r).account, in.IDs)
	if status != 200 {
		fail(w, status)
		return
	}
	reply(w, 200, syncwire.RelayPrepared{RelayState: state, IDs: needed.IDs})
}
func (s *Server) relayNeedBatch(w http.ResponseWriter, r *http.Request) {
	var in syncwire.RelayNeed
	if !decode(w, r, 16384, &in) {
		return
	}
	if !validRelayIDs(in.IDs) {
		fail(w, 400)
		return
	}
	out, status := s.neededRelay(who(r).account, in.IDs)
	if status != 200 {
		fail(w, status)
		return
	}
	reply(w, 200, out)
}
