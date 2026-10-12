// Package server stores Klefki ciphertext and authentication verifiers.
package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

type Config struct {
	DataDir string
	Logger  *slog.Logger
	// AllowHTTP is only for explicitly configured loopback development listeners.
	AllowHTTP     bool
	TrustProxy    bool
	RelayMaxBytes int64
	RateBurst     int
	SessionTTL    time.Duration
	Now           func() time.Time
}
type Server struct {
	db         *sql.DB
	blobs      BlobStore
	config     Config
	handler    http.Handler
	mu         sync.Mutex
	relayLocks accountLocks
	buckets    map[string]bucket
	presenceMu sync.Mutex
	presences  map[string]syncwire.DeviceStatus
}
type bucket struct {
	tokens float64
	at     time.Time
}
type Signup struct {
	Account         string `json:"account"`
	Descriptor      []byte `json:"descriptor"`
	PasswordWrapper []byte `json:"password_wrapper"`
	RecoveryWrapper []byte `json:"recovery_wrapper"`
	Credential      []byte `json:"credential"`
}
type Login struct {
	Account    string `json:"account"`
	Credential []byte `json:"credential"`
}
type Session struct {
	Token   string `json:"token"`
	Device  string `json:"device"`
	Expires int64  `json:"expires"`
}
type Record struct {
	Metadata []byte `json:"metadata"`
	FileKey  []byte `json:"file_key,omitempty"`
}
type identity struct{ account, device string }
type identityKey struct{}

func Open(c Config) (*Server, error) {
	if c.DataDir == "" {
		return nil, errors.New("data directory required")
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.RateBurst <= 0 {
		c.RateBurst = 120
	}
	if c.RelayMaxBytes <= 0 {
		c.RelayMaxBytes = 256 * 1024 * 1024
	}
	if c.SessionTTL <= 0 {
		c.SessionTTL = 30 * 24 * time.Hour
	}
	if e := os.MkdirAll(c.DataDir, 0700); e != nil {
		return nil, e
	}
	// Create privately before SQLite opens the database and its journal files.
	f, e := os.OpenFile(filepath.Join(c.DataDir, "klefki.db"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite", filepath.Join(c.DataDir, "klefki.db"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA auto_vacuum=INCREMENTAL; PRAGMA secure_delete=ON; PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY, descriptor BLOB NOT NULL,password_wrapper BLOB NOT NULL,recovery_wrapper BLOB NOT NULL,verifier BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(device TEXT PRIMARY KEY,account TEXT NOT NULL REFERENCES accounts(id),token_hash BLOB UNIQUE NOT NULL,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS metadata(account TEXT NOT NULL REFERENCES accounts(id),id TEXT NOT NULL,metadata BLOB NOT NULL,file_key BLOB,PRIMARY KEY(account,id));`)
	if e != nil {
		db.Close()
		return nil, e
	}
	if e = migrateSync(db); e != nil {
		db.Close()
		return nil, e
	}
	if e = migrateRelay(db); e != nil {
		db.Close()
		return nil, e
	}
	if e = migrateStatus(db); e != nil {
		db.Close()
		return nil, e
	}
	blobs, e := NewDiskStore(filepath.Join(c.DataDir, "blobs"))
	if e != nil {
		db.Close()
		return nil, e
	}
	s := &Server{db: db, blobs: blobs, config: c, buckets: make(map[string]bucket), presences: make(map[string]syncwire.DeviceStatus)}
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if s.db.PingContext(r.Context()) != nil {
			fail(w, 503)
			return
		}
		w.WriteHeader(200)
	})
	r.Post("/v1/signup", s.signup)
	r.Post("/v1/login", s.login)
	r.Get("/v1/accounts/{account}", s.account)
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.Post("/v1/sync/presence", s.presence)
		r.Get("/v1/sync/status", s.syncStatus)
		r.Get("/v1/sync/provenance", s.provenance)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.Use(s.relayBuffered)
		r.Use(s.relayAccess)
		r.Get("/v1/sync/settings", s.getSettings)
		r.Put("/v1/sync/settings", s.putSettings)
		r.Post("/v1/sync/relay/device", s.relayDevice)
		r.Post("/v1/sync/relay/need", s.relayNeed)
		r.Post("/v1/sync/relay/prepare", s.relayPrepare)
		r.Post("/v1/sync/relay/need-batch", s.relayNeedBatch)
		r.Delete("/v1/sync/relay/devices/{device}", s.forgetRelayDevice)
		r.Get("/v1/devices", s.devices)
		r.Delete("/v1/devices/{device}", s.revoke)
		r.Put("/v1/blobs/{blob}/chunks/{index}", s.putChunk)
		r.Get("/v1/blobs/{blob}/chunks/{index}", s.getChunk)
		r.Get("/v1/blobs/{blob}/chunks", s.listChunks)
		r.Put("/v1/metadata/{object}", s.putMetadata)
		r.Get("/v1/metadata/{object}", s.getMetadata)
		r.Get("/v1/metadata", s.listMetadata)
		r.Put("/v1/sync/revisions/{revision}", s.putRevision)
		r.Get("/v1/sync/changes", s.changes)
		r.Get("/v1/sync/cursor", s.getCursor)
		r.Put("/v1/sync/cursor", s.putCursor)
	})
	s.handler = r
	return s, nil
}
func (s *Server) Close() error               { return s.db.Close() }
func fail(w http.ResponseWriter, status int) { http.Error(w, http.StatusText(status), status) }
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func validID(x string) bool {
	b, e := hex.DecodeString(x)
	return e == nil && len(b) == 16 && x == strings.ToLower(x) && !bytes.Equal(b, make([]byte, 16))
}
func envelope(b []byte, kind byte, account, object string) bool {
	if len(b) < 74 || b[0] != 1 || b[1] != kind || hex.EncodeToString(b[2:18]) != account || hex.EncodeToString(b[18:34]) != object {
		return false
	}
	if kind <= 3 {
		return len(b) == 106
	}
	return kind == 4 && len(b) <= vc.MaxMetadata+74
}
func readBody(w http.ResponseWriter, r *http.Request, n int64) ([]byte, bool) {
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, n))
	if e != nil {
		var limit *http.MaxBytesError
		if errors.As(e, &limit) {
			fail(w, 413)
		} else {
			fail(w, 400)
		}
		return nil, false
	}
	return b, true
}
func decode(w http.ResponseWriter, r *http.Request, n int64, v any) bool {
	b, ok := readBody(w, r, n)
	if !ok {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, 400)
		return false
	}
	return true
}
func (s *Server) allowed(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.config.Now()
	b, ok := s.buckets[key]
	if !ok {
		if len(s.buckets) >= 10000 {
			for k, v := range s.buckets {
				if now.Sub(v.at) > time.Minute {
					delete(s.buckets, k)
				}
			}
			if len(s.buckets) >= 10000 {
				return false
			}
		}
		b = bucket{float64(s.config.RateBurst), now}
	}
	b.tokens = min(float64(s.config.RateBurst), b.tokens+now.Sub(b.at).Seconds()*float64(s.config.RateBurst)/60)
	b.at = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	s.buckets[key] = b
	return allowed
}

type responseStatus struct {
	http.ResponseWriter
	status int
}

func (w *responseStatus) WriteHeader(n int) {
	if w.status == 0 {
		w.status = n
		w.ResponseWriter.WriteHeader(n)
	}
}
func (w *responseStatus) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sw := &responseStatus{ResponseWriter: w}
	start := time.Now()
	defer func() {
		if recover() != nil {
			fail(sw, 500)
		}
		status := sw.status
		if status == 0 {
			status = 200
		}
		s.config.Logger.Info("request", "status", status, "duration_ms", time.Since(start).Milliseconds())
	}()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.TLS == nil && !s.config.AllowHTTP && !(s.config.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https") && !(r.Method == http.MethodGet && r.URL.Path == "/healthz") {
		fail(sw, 426)
		return
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	// Do not trust forwarding headers. Use separate public-auth and data budgets.
	group := "data:"
	if r.URL.Path == "/v1/login" || r.URL.Path == "/v1/signup" || strings.HasPrefix(r.URL.Path, "/v1/accounts/") {
		group = "auth:"
	}
	if r.URL.Path == "/v1/sync/presence" || r.URL.Path == "/v1/sync/status" || r.URL.Path == "/v1/sync/provenance" {
		group = "status:"
	}
	if !s.allowed(group + host) {
		sw.Header().Set("Retry-After", "60")
		fail(sw, 429)
		return
	}
	s.handler.ServeHTTP(sw, r)
}
func newSession(now time.Time, ttl time.Duration) (Session, []byte, error) {
	token := make([]byte, 32)
	if _, e := rand.Read(token); e != nil {
		return Session{}, nil, e
	}
	id, e := vc.RandomID()
	if e != nil {
		return Session{}, nil, e
	}
	hash := sha256.Sum256(token)
	return Session{hex.EncodeToString(token), hex.EncodeToString(id[:]), now.Add(ttl).Unix()}, hash[:], nil
}
func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var in Signup
	if !decode(w, r, 8192, &in) {
		return
	}
	defer vc.Wipe(in.Credential)
	zero := strings.Repeat("0", 32)
	if !validID(in.Account) || len(in.Credential) != 32 || vc.ValidateDescriptor(in.Descriptor) != nil || !envelope(in.PasswordWrapper, 1, in.Account, zero) || !envelope(in.RecoveryWrapper, 2, in.Account, zero) {
		fail(w, 400)
		return
	}
	session, hash, e := newSession(s.config.Now(), s.config.SessionTTL)
	if e != nil {
		fail(w, 500)
		return
	}
	verifier := vc.Verifier(vc.Key(in.Credential))
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500)
		return
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(r.Context(), `INSERT INTO accounts VALUES(?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, in.Account, in.Descriptor, in.PasswordWrapper, in.RecoveryWrapper, verifier[:])
	if e != nil {
		fail(w, 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 409)
		return
	}
	if _, e = tx.ExecContext(r.Context(), `INSERT INTO sessions VALUES(?,?,?,?)`, session.Device, in.Account, hash, session.Expires); e != nil {
		fail(w, 500)
		return
	}
	if tx.Commit() != nil {
		fail(w, 500)
		return
	}
	reply(w, 201, session)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in Login
	if !decode(w, r, 8192, &in) {
		return
	}
	defer vc.Wipe(in.Credential)
	if !validID(in.Account) || len(in.Credential) != 32 {
		fail(w, 400)
		return
	}
	if !s.allowed("account:" + in.Account) {
		fail(w, 429)
		return
	}
	expected := make([]byte, 32)
	e := s.db.QueryRowContext(r.Context(), `SELECT verifier FROM accounts WHERE id=?`, in.Account).Scan(&expected)
	if e != nil && e != sql.ErrNoRows {
		fail(w, 500)
		return
	}
	actual := vc.Verifier(vc.Key(in.Credential))
	match := subtle.ConstantTimeCompare(expected, actual[:])
	if match != 1 || e == sql.ErrNoRows {
		fail(w, 401)
		return
	}
	session, hash, e := newSession(s.config.Now(), s.config.SessionTTL)
	if e != nil {
		fail(w, 500)
		return
	}
	_, e = s.db.ExecContext(r.Context(), `INSERT INTO sessions VALUES(?,?,?,?)`, session.Device, in.Account, hash, session.Expires)
	if e != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, session)
}
func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "account")
	if !validID(id) {
		fail(w, 404)
		return
	}
	var out struct {
		Account         string `json:"account"`
		Descriptor      []byte `json:"descriptor"`
		PasswordWrapper []byte `json:"password_wrapper"`
		RecoveryWrapper []byte `json:"recovery_wrapper"`
	}
	out.Account = id
	e := s.db.QueryRowContext(r.Context(), `SELECT descriptor,password_wrapper,recovery_wrapper FROM accounts WHERE id=?`, id).Scan(&out.Descriptor, &out.PasswordWrapper, &out.RecoveryWrapper)
	if e == sql.ErrNoRows {
		fail(w, 404)
		return
	}
	if e != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			fail(w, 401)
			return
		}
		token, e := hex.DecodeString(strings.TrimPrefix(h, "Bearer "))
		if e != nil || len(token) != 32 {
			fail(w, 401)
			return
		}
		hash := sha256.Sum256(token)
		vc.Wipe(token)
		var id identity
		e = s.db.QueryRowContext(r.Context(), `SELECT account,device FROM sessions WHERE token_hash=? AND expires>?`, hash[:], s.config.Now().Unix()).Scan(&id.account, &id.device)
		if e == sql.ErrNoRows {
			fail(w, 401)
			return
		}
		if e != nil {
			fail(w, 500)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}
func who(r *http.Request) identity { return r.Context().Value(identityKey{}).(identity) }
func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	after, ok := cursor(w, r)
	if !ok {
		return
	}
	rows, e := s.db.QueryContext(r.Context(), `SELECT device,expires FROM sessions WHERE account=? AND expires>? AND device>? ORDER BY device LIMIT 1000`, who(r).account, s.config.Now().Unix(), after)
	if e != nil {
		fail(w, 500)
		return
	}
	defer rows.Close()
	type device struct {
		ID      string `json:"id"`
		Expires int64  `json:"expires"`
	}
	out := []device{}
	for rows.Next() {
		var d device
		if rows.Scan(&d.ID, &d.Expires) != nil {
			fail(w, 500)
			return
		}
		out = append(out, d)
	}
	if rows.Err() != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "device")
	if !validID(id) {
		fail(w, 400)
		return
	}
	res, e := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE account=? AND device=?`, who(r).account, id)
	if e != nil {
		fail(w, 500)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404)
		return
	}
	w.WriteHeader(204)
}
func chunkParams(w http.ResponseWriter, r *http.Request) (string, uint64, bool) {
	id := chi.URLParam(r, "blob")
	raw := chi.URLParam(r, "index")
	n, e := strconv.ParseUint(raw, 10, 64)
	if !validID(id) || e != nil || strconv.FormatUint(n, 10) != raw || n >= vc.ChunkCount(vc.MaxFileSize) {
		fail(w, 400)
		return "", 0, false
	}
	return id, n, true
}
func (s *Server) putChunk(w http.ResponseWriter, r *http.Request) {
	id, index, ok := chunkParams(w, r)
	if !ok {
		return
	}
	b, ok := readBody(w, r, vc.ChunkSize+87)
	if !ok {
		return
	}
	if len(b) < 87 || b[0] != 1 || b[1] != 5 || hex.EncodeToString(b[2:18]) != who(r).account || hex.EncodeToString(b[18:34]) != id || binary.BigEndian.Uint64(b[34:42]) != index || b[42] > 1 || int(binary.BigEndian.Uint32(b[43:47])) != len(b)-87 || (b[42] == 0 && len(b) != vc.ChunkSize+87) || (len(b) == 87 && (index != 0 || b[42] != 1)) {
		fail(w, 400)
		return
	}
	if e := s.relayChunk(who(r).account, id, index, b); e != nil {
		if errors.Is(e, errRelayFull) {
			fail(w, 507)
		} else {
			fail(w, 500)
		}
		return
	}
	created, e := s.blobs.Put(who(r).account, id, index, b)
	if errors.Is(e, ErrConflict) {
		fail(w, 409)
		return
	}
	if e != nil {
		fail(w, 500)
		return
	}
	if created {
		w.WriteHeader(201)
	} else {
		w.WriteHeader(204)
	}
}
func (s *Server) getChunk(w http.ResponseWriter, r *http.Request) {
	id, index, ok := chunkParams(w, r)
	if !ok {
		return
	}
	b, e := s.blobs.Get(who(r).account, id, index)
	if errors.Is(e, os.ErrNotExist) {
		fail(w, 404)
		return
	}
	if e != nil {
		fail(w, 500)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(b)
}
func cursor(w http.ResponseWriter, r *http.Request) (string, bool) {
	a := r.URL.Query().Get("after")
	if a != "" && !validID(a) {
		fail(w, 400)
		return "", false
	}
	return a, true
}
func (s *Server) listChunks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "blob")
	if !validID(id) {
		fail(w, 400)
		return
	}
	after := r.URL.Query().Get("after")
	var n int64 = -1
	if after != "" {
		var e error
		n, e = strconv.ParseInt(after, 10, 64)
		if e != nil || n < 0 || uint64(n) >= vc.ChunkCount(vc.MaxFileSize) {
			fail(w, 400)
			return
		}
	}
	if settings, e := s.settings(who(r).account); e != nil {
		fail(w, 500)
		return
	} else if settings.Mode == syncwire.TemporaryRelay {
		if _, e = s.db.Exec(`INSERT OR IGNORE INTO relay_uploads VALUES(?,?)`, who(r).account, id); e != nil {
			fail(w, 500)
			return
		}
	}
	out, e := s.blobs.List(who(r).account, id, n)
	if e != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}
func (s *Server) putMetadata(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "object")
	var in Record
	if !decode(w, r, 1500000, &in) {
		return
	}
	account := who(r).account
	if !validID(id) || !envelope(in.Metadata, 4, account, id) || (len(in.FileKey) != 0 && (len(in.FileKey) != 106 || !validID(hex.EncodeToString(in.FileKey[18:34])) || !envelope(in.FileKey, 3, account, hex.EncodeToString(in.FileKey[18:34])))) {
		fail(w, 400)
		return
	}
	// Publish the legacy record and its change-log entry in one transaction.
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500)
		return
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(r.Context(), `INSERT INTO metadata VALUES(?,?,?,?) ON CONFLICT(account,id) DO NOTHING`, account, id, in.Metadata, in.FileKey)
	if e != nil {
		fail(w, 500)
		return
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		if _, _, e = appendChange(r.Context(), tx, account, syncwire.Revision{ID: id, Object: id, Metadata: in.Metadata, FileKey: in.FileKey}); e != nil {
			if errors.Is(e, errRevisionConflict) {
				fail(w, 409)
			} else {
				fail(w, 500)
			}
			return
		}
		if tx.Commit() != nil {
			fail(w, 500)
			return
		}
		w.WriteHeader(201)
		return
	}
	var old Record
	e = tx.QueryRowContext(r.Context(), `SELECT metadata,file_key FROM metadata WHERE account=? AND id=?`, account, id).Scan(&old.Metadata, &old.FileKey)
	if e != nil {
		fail(w, 500)
		return
	}
	if !bytes.Equal(old.Metadata, in.Metadata) || !bytes.Equal(old.FileKey, in.FileKey) {
		fail(w, 409)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) getMetadata(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "object")
	if !validID(id) {
		fail(w, 400)
		return
	}
	var out Record
	e := s.db.QueryRowContext(r.Context(), `SELECT metadata,file_key FROM metadata WHERE account=? AND id=?`, who(r).account, id).Scan(&out.Metadata, &out.FileKey)
	if e == sql.ErrNoRows {
		fail(w, 404)
		return
	}
	if e != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}
func (s *Server) listMetadata(w http.ResponseWriter, r *http.Request) {
	after, ok := cursor(w, r)
	if !ok {
		return
	}
	rows, e := s.db.QueryContext(r.Context(), `SELECT id FROM metadata WHERE account=? AND id>? ORDER BY id LIMIT 1000`, who(r).account, after)
	if e != nil {
		fail(w, 500)
		return
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			fail(w, 500)
			return
		}
		out = append(out, id)
	}
	if rows.Err() != nil {
		fail(w, 500)
		return
	}
	reply(w, 200, out)
}
