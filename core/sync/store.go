// Package sync provides a ciphertext-only local store and resumable synchronization.
package sync

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"

	vc "klefki/core/crypto"
	"klefki/core/local"
	"klefki/core/syncwire"
)

var (
	ErrStore    = errors.New("local encrypted store failed")
	ErrInvalid  = errors.New("invalid sync data")
	ErrConflict = errors.New("sync revision conflict")
	ErrRemote   = errors.New("sync request failed")
)

type Store struct {
	db        *sql.DB
	account   vc.ID
	mu        sync.Mutex
	syncMu    sync.Mutex
	lease     *local.Lease
	closeOnce sync.Once
	closeErr  error
}
type Head struct {
	Object   string
	Revision string
	Metadata vc.Metadata
	Conflict bool
}

func Open(path string, account vc.ID) (*Store, error) {
	return openStore(path, account, false)
}

// OpenExisting refuses missing or uninitialized stores, for backup and recovery.
func OpenExisting(path string, account vc.ID) (*Store, error) {
	return openStore(path, account, true)
}

func openStore(path string, account vc.ID, existing bool) (*Store, error) {
	if account == (vc.ID{}) {
		return nil, ErrInvalid
	}
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return nil, ErrStore
	}
	flags := os.O_RDWR
	if !existing {
		flags |= os.O_CREATE
	}
	f, e := os.OpenFile(path, flags, 0600)
	if e != nil {
		return nil, ErrStore
	}
	f.Close()
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil {
		return nil, ErrStore
	}
	lease, e := local.Acquire(canonical + ".lock")
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			lease.Close()
		}
	}()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, ErrStore
	}
	db.SetMaxOpenConns(1)
	fail := func() (*Store, error) { db.Close(); return nil, ErrStore }
	if existing {
		var version int
		var id string
		if db.QueryRow(`SELECT version,account FROM local_state WHERE singleton=1`).Scan(&version, &id) != nil || version != 1 || id != account.String() {
			return fail()
		}
		for _, query := range []string{`SELECT ordinal,id,object,base,metadata,file_key,pending FROM revisions LIMIT 0`, `SELECT file,idx,data FROM chunks LIMIT 0`} {
			rows, e := db.Query(query)
			if e != nil {
				return fail()
			}
			rows.Close()
		}
	}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS local_state(singleton INTEGER PRIMARY KEY CHECK(singleton=1),version INTEGER NOT NULL,account TEXT NOT NULL,remote TEXT NOT NULL,cursor INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS revisions(ordinal INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,object TEXT NOT NULL,base TEXT NOT NULL,metadata BLOB NOT NULL,file_key BLOB,pending INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS revision_parent ON revisions(base);
 CREATE TABLE IF NOT EXISTS chunks(file TEXT NOT NULL,idx INTEGER NOT NULL,data BLOB NOT NULL,PRIMARY KEY(file,idx));`)
	if e != nil {
		return fail()
	}
	_, e = db.Exec(`INSERT OR IGNORE INTO local_state VALUES(1,1,?,'',0)`, account.String())
	if e != nil {
		return fail()
	}
	var version int
	var id string
	if db.QueryRow(`SELECT version,account FROM local_state WHERE singleton=1`).Scan(&version, &id) != nil {
		return fail()
	}
	if version != 1 || id != account.String() {
		db.Close()
		return nil, ErrInvalid
	}
	store := &Store{db: db, account: account, lease: lease}
	if _, e = store.RelayState(); e != nil {
		return fail()
	}
	if _, e = db.Exec(`CREATE TABLE IF NOT EXISTS revision_sources(revision TEXT PRIMARY KEY,origin TEXT NOT NULL);
 CREATE TRIGGER IF NOT EXISTS revision_origin AFTER INSERT ON revisions WHEN NEW.pending=1 BEGIN
 INSERT INTO revision_sources SELECT NEW.id,device FROM relay_local WHERE id=1; END;`); e != nil {
		return fail()
	}
	success = true
	return store, nil
}
func (s *Store) Close() error {
	s.closeOnce.Do(func() { s.mu.Lock(); defer s.mu.Unlock(); s.closeErr = s.db.Close(); s.lease.Close() })
	return s.closeErr
}
func (s *Store) Pending() (int, error) {
	var n int
	if s.db.QueryRow(`SELECT COUNT(*) FROM revisions WHERE pending=1`).Scan(&n) != nil {
		return 0, ErrStore
	}
	return n, nil
}
func (s *Store) Cursor() (int64, error) {
	var n int64
	if s.db.QueryRow(`SELECT cursor FROM local_state WHERE singleton=1`).Scan(&n) != nil {
		return 0, ErrStore
	}
	return n, nil
}
func (s *Store) bind(remote string) error {
	res, e := s.db.Exec(`UPDATE local_state SET remote=? WHERE singleton=1 AND (remote='' OR remote=?)`, remote, remote)
	if e != nil {
		return ErrStore
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("local store belongs to a different sync server")
	}
	return nil
}
func parseID(id string) (vc.ID, error) {
	x, e := vc.ParseID(id)
	if e != nil {
		return x, ErrInvalid
	}
	return x, nil
}
func (s *Store) Put(master vc.Key, object, base string, m vc.Metadata, source io.Reader) (string, error) {
	return s.PutContext(context.Background(), master, object, base, m, source)
}
func (s *Store) PutContext(ctx context.Context, master vc.Key, object, base string, m vc.Metadata, source io.Reader) (string, error) {
	if e := ctx.Err(); e != nil {
		return "", e
	}
	if source != nil {
		source = &contextReader{ctx, source}
	}
	defer vc.Wipe(master[:])
	oid, e := parseID(object)
	if e != nil {
		return "", e
	}
	if base != "" {
		if _, e = parseID(base); e != nil {
			return "", e
		}
	}
	rid, e := vc.RandomID()
	if e != nil {
		return "", e
	}
	var file vc.ID
	var key vc.Key
	var wrapped []byte
	if m.Type == "file" {
		if source == nil || m.Size > vc.MaxFileSize {
			return "", ErrInvalid
		}
		file, e = vc.RandomID()
		if e != nil {
			return "", e
		}
		key, e = vc.RandomKey()
		if e != nil {
			return "", e
		}
		defer vc.Wipe(key[:])
		m.FileID = file.String()
		m.ChunkCount = vc.ChunkCount(m.Size)
		wrapped, e = vc.Seal(master, vc.Context{Kind: vc.FileKey, Account: s.account, Object: file}, key[:], nil)
		if e != nil {
			return "", e
		}
	} else if m.Type != "folder" || source != nil {
		return "", ErrInvalid
	}
	encrypted, e := vc.EncryptMetadata(master, s.account, oid, m)
	if e != nil {
		return "", e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return "", ErrStore
	}
	defer tx.Rollback()
	if base != "" {
		var oldObject string
		var metadata []byte
		e = tx.QueryRow(`SELECT object,metadata FROM revisions WHERE id=?`, base).Scan(&oldObject, &metadata)
		if e != nil || oldObject != object {
			return "", ErrConflict
		}
		if _, e = vc.DecryptMetadata(master, s.account, oid, metadata); e != nil {
			return "", e
		}
	}
	if m.Type == "file" {
		writer := &chunkWriter{tx: tx, file: file.String()}
		if e = vc.EncryptStream(key, s.account, file, m.Size, source, writer); e != nil {
			return "", e
		}
	}
	_, e = tx.Exec(`INSERT INTO revisions(id,object,base,metadata,file_key,pending) VALUES(?,?,?,?,?,1)`, rid.String(), object, base, encrypted, wrapped)
	if e != nil {
		return "", ErrStore
	}
	if e := ctx.Err(); e != nil {
		return "", e
	}
	if tx.Commit() != nil {
		return "", ErrStore
	}
	return rid.String(), nil
}

type chunkWriter struct {
	tx    *sql.Tx
	file  string
	index uint64
}

func (w *chunkWriter) Write(b []byte) (int, error) {
	_, e := w.tx.Exec(`INSERT INTO chunks VALUES(?,?,?)`, w.file, w.index, b)
	if e != nil {
		return 0, ErrStore
	}
	w.index++
	return len(b), nil
}
func (s *Store) Heads(master vc.Key) ([]Head, error) {
	defer vc.Wipe(master[:])
	rows, e := s.db.Query(`SELECT r.id,r.object,r.metadata FROM revisions r WHERE NOT EXISTS(SELECT 1 FROM revisions child WHERE child.base=r.id) ORDER BY r.object,r.id`)
	if e != nil {
		return nil, ErrStore
	}
	defer rows.Close()
	out := []Head{}
	counts := map[string]int{}
	for rows.Next() {
		var h Head
		var data []byte
		if rows.Scan(&h.Revision, &h.Object, &data) != nil {
			return nil, ErrStore
		}
		id, e := parseID(h.Object)
		if e != nil {
			return nil, e
		}
		h.Metadata, e = vc.DecryptMetadata(master, s.account, id, data)
		if e != nil {
			return nil, e
		}
		counts[h.Object]++
		out = append(out, h)
	}
	if rows.Err() != nil {
		return nil, ErrStore
	}
	for i := range out {
		out[i].Conflict = counts[out[i].Object] > 1
	}
	return out, nil
}
func readRevision(row *sql.Row) (syncwire.Revision, error) {
	var r syncwire.Revision
	e := row.Scan(&r.ID, &r.Object, &r.Base, &r.Metadata, &r.FileKey)
	return r, e
}
func (s *Store) unlock(master vc.Key, r syncwire.Revision) (vc.Metadata, vc.ID, vc.Key, error) {
	defer vc.Wipe(master[:])
	object, e := parseID(r.Object)
	if e != nil {
		return vc.Metadata{}, vc.ID{}, vc.Key{}, e
	}
	m, e := vc.DecryptMetadata(master, s.account, object, r.Metadata)
	if e != nil {
		return m, vc.ID{}, vc.Key{}, e
	}
	var file vc.ID
	var key vc.Key
	if m.Type == "folder" {
		if len(r.FileKey) != 0 {
			return m, file, key, ErrInvalid
		}
		return m, file, key, nil
	}
	file, e = parseID(m.FileID)
	if e != nil {
		return m, file, key, e
	}
	raw, e := vc.Open(master, vc.Context{Kind: vc.FileKey, Account: s.account, Object: file}, r.FileKey, nil)
	if e != nil {
		return m, file, key, e
	}
	copy(key[:], raw)
	vc.Wipe(raw)
	return m, file, key, nil
}
func (s *Store) Export(master vc.Key, revision string, out io.Writer) error {
	return s.ExportContext(context.Background(), master, revision, out)
}
func (s *Store) ExportContext(ctx context.Context, master vc.Key, revision string, out io.Writer) error {
	defer vc.Wipe(master[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return ErrStore
	}
	defer tx.Rollback()
	r, e := readRevision(tx.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, revision))
	if e != nil {
		return ErrStore
	}
	m, file, key, e := s.unlock(master, r)
	if e != nil {
		return e
	}
	defer vc.Wipe(key[:])
	if m.Type != "file" {
		return ErrInvalid
	}
	var count uint64
	if tx.QueryRow(`SELECT COUNT(*) FROM chunks WHERE file=?`, file.String()).Scan(&count) != nil {
		return ErrStore
	}
	if count != m.ChunkCount {
		return ErrInvalid
	}
	// Validate the entire file before releasing plaintext. Both passes share a
	// stable transaction snapshot; callers still stage output for writer failures.
	for _, dest := range []io.Writer{io.Discard, out} {
		reader := &chunkReader{tx: tx, file: file.String(), count: count}
		if e = vc.DecryptStream(key, s.account, file, m.Size, &contextReader{ctx, reader}, &contextWriter{ctx, dest}); e != nil {
			return e
		}
	}
	return nil
}

type chunkReader struct {
	tx           *sql.Tx
	file         string
	count, index uint64
	buffer       *bytes.Reader
}

func (r *chunkReader) Read(p []byte) (int, error) {
	for r.buffer == nil || r.buffer.Len() == 0 {
		if r.index >= r.count {
			return 0, io.EOF
		}
		var b []byte
		if r.tx.QueryRow(`SELECT data FROM chunks WHERE file=? AND idx=?`, r.file, r.index).Scan(&b) != nil {
			return 0, ErrStore
		}
		r.index++
		r.buffer = bytes.NewReader(b)
	}
	return r.buffer.Read(p)
}
func (s *Store) nextPending() (syncwire.Revision, error) {
	return readRevision(s.db.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE pending=1 ORDER BY ordinal LIMIT 1`))
}
func (s *Store) chunk(file string, index uint64) ([]byte, error) {
	var b []byte
	if s.db.QueryRow(`SELECT data FROM chunks WHERE file=? AND idx=?`, file, index).Scan(&b) != nil {
		return nil, ErrStore
	}
	return b, nil
}
func (s *Store) uploaded(id string) error {
	if _, e := s.db.Exec(`UPDATE revisions SET pending=0 WHERE id=?`, id); e != nil {
		return ErrStore
	}
	return nil
}
func sameRevision(a, b syncwire.Revision) bool {
	return a.ID == b.ID && a.Object == b.Object && a.Base == b.Base && bytes.Equal(a.Metadata, b.Metadata) && bytes.Equal(a.FileKey, b.FileKey)
}
func (s *Store) apply(ctx context.Context, master vc.Key, c syncwire.Change, fetch func(string, uint64) ([]byte, error)) error {
	return s.applyTracked(ctx, master, c, "", nil, func(_ context.Context, file string, index uint64) ([]byte, error) { return fetch(file, index) })
}
func (s *Store) applyTracked(ctx context.Context, master vc.Key, c syncwire.Change, origin string, progress func(uint64, uint64, bool), fetch func(context.Context, string, uint64) ([]byte, error)) error {
	defer vc.Wipe(master[:])
	if c.Sequence <= 0 {
		return ErrInvalid
	}
	if _, e := parseID(c.Revision.ID); e != nil {
		return e
	}
	if c.Revision.Base == c.Revision.ID {
		return ErrInvalid
	}
	m, file, key, e := s.unlock(master, c.Revision)
	if e != nil {
		return e
	}
	defer vc.Wipe(key[:])
	// Stage authenticated ciphertext before opening the local write transaction.
	// Network delays must not hold the database connection or hide the library.
	var staged *os.File
	if m.Type == "file" {
		staged, e = os.CreateTemp("", "klefki-sync-*")
		if e != nil {
			return ErrStore
		}
		defer os.Remove(staged.Name())
		defer staged.Close()

		type downloaded struct {
			data   []byte
			size   uint64
			reused bool
		}
		e = chunkWindows(ctx, m.ChunkCount, func(ctx context.Context, i uint64) (downloaded, error) {
			var b []byte
			err := s.db.QueryRowContext(ctx, `SELECT data FROM chunks WHERE file=? AND idx=?`, file.String(), i).Scan(&b)
			reused := err == nil
			if err == sql.ErrNoRows {
				b, err = fetch(ctx, file.String(), i)
			}
			if err != nil {
				return downloaded{}, err
			}
			plain, err := vc.Open(key, vc.Context{Kind: vc.Chunk, Account: s.account, Object: file, Index: i, Final: i == m.ChunkCount-1}, b, nil)
			if err != nil {
				return downloaded{}, err
			}
			length := uint64(len(plain))
			vc.Wipe(plain)
			if length != min(uint64(vc.ChunkSize), m.Size-i*vc.ChunkSize) {
				return downloaded{}, ErrInvalid
			}
			return downloaded{b, length, reused}, nil
		}, func(i uint64, chunk downloaded) error {
			if _, err := staged.Write(chunk.data); err != nil {
				return ErrStore
			}
			if progress != nil {
				progress(i*vc.ChunkSize+chunk.size, i+1, chunk.reused)
			}
			return nil
		})
		if e != nil {
			return e
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return ErrStore
	}
	defer tx.Rollback()
	var cursor int64
	if tx.QueryRow(`SELECT cursor FROM local_state WHERE singleton=1`).Scan(&cursor) != nil {
		return ErrStore
	}
	if c.Sequence <= cursor {
		return ErrInvalid
	}
	if c.Revision.Base != "" {
		var parent string
		if tx.QueryRow(`SELECT object FROM revisions WHERE id=?`, c.Revision.Base).Scan(&parent) != nil || parent != c.Revision.Object {
			return ErrInvalid
		}
	}
	old, e := readRevision(tx.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, c.Revision.ID))
	if e == nil && !sameRevision(old, c.Revision) {
		return ErrConflict
	}
	if e != nil && e != sql.ErrNoRows {
		return ErrStore
	}
	if staged != nil {
		if _, e = staged.Seek(0, io.SeekStart); e != nil {
			return ErrStore
		}
		left := m.Size
		for i := uint64(0); i < m.ChunkCount; i++ {
			length := min(uint64(vc.ChunkSize), left)
			b := make([]byte, length+87)
			if _, e = io.ReadFull(staged, b); e != nil {
				return ErrStore
			}
			var existing []byte
			err := tx.QueryRow(`SELECT data FROM chunks WHERE file=? AND idx=?`, file.String(), i).Scan(&existing)
			if err == nil && !bytes.Equal(existing, b) {
				return ErrConflict
			}
			if err != nil && err != sql.ErrNoRows {
				return ErrStore
			}
			if _, e = tx.Exec(`INSERT OR IGNORE INTO chunks VALUES(?,?,?)`, file.String(), i, b); e != nil {
				return ErrStore
			}
			left -= length
		}
		var count uint64
		if tx.QueryRow(`SELECT COUNT(*) FROM chunks WHERE file=?`, file.String()).Scan(&count) != nil {
			return ErrStore
		}
		if count != m.ChunkCount {
			return ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	_, e = tx.Exec(`INSERT INTO revisions(id,object,base,metadata,file_key,pending) VALUES(?,?,?,?,?,0) ON CONFLICT(id) DO UPDATE SET pending=0`, c.Revision.ID, c.Revision.Object, c.Revision.Base, c.Revision.Metadata, c.Revision.FileKey)
	if e != nil {
		return ErrStore
	}
	if origin != "" {
		if _, e = tx.Exec(`INSERT OR IGNORE INTO revision_sources VALUES(?,?)`, c.Revision.ID, origin); e != nil {
			return ErrStore
		}
	}
	if _, e = tx.Exec(`UPDATE local_state SET cursor=? WHERE singleton=1`, c.Sequence); e != nil {
		return ErrStore
	}
	if tx.Commit() != nil {
		return ErrStore
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}

type contextWriter struct {
	ctx context.Context
	w   io.Writer
}

func (w *contextWriter) Write(b []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	return w.w.Write(b)
}
