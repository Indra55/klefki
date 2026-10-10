package sync

import (
	"io"
	"os"
	"path/filepath"

	vc "klefki/core/crypto"
	"klefki/core/local"
	"klefki/core/syncwire"
)

// Snapshot copies a consistent SQLite snapshot, including pending work and history.
// The destination must not exist. No master key or plaintext enters the snapshot.
func (s *Store) Snapshot(path string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, e := os.MkdirTemp(filepath.Dir(path), ".klefki-snapshot-*")
	if e != nil {
		return ErrStore
	}
	defer os.RemoveAll(dir)
	temp := filepath.Join(dir, "local.db")
	if _, e = s.db.Exec(`VACUUM INTO ?`, temp); e != nil {
		return ErrStore
	}
	return local.Publish(path, func(w io.Writer) error {
		f, e := os.Open(temp)
		if e != nil {
			return ErrStore
		}
		defer f.Close()
		if _, e = io.Copy(w, f); e != nil {
			return ErrStore
		}
		return nil
	})
}

// Check authenticates all revisions and every stored chunk, including history.
// Like the v1 format, it cannot detect rollback or deletion of a complete branch.
func (s *Store) Check(master vc.Key) ([]Revision, error) {
	defer vc.Wipe(master[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return nil, ErrStore
	}
	defer tx.Rollback()
	var result string
	if tx.QueryRow(`PRAGMA integrity_check`).Scan(&result) != nil || result != "ok" {
		return nil, ErrInvalid
	}
	rows, e := tx.Query(`SELECT id,object,base,metadata,file_key FROM revisions ORDER BY ordinal`)
	if e != nil {
		return nil, ErrStore
	}
	records := []syncwire.Revision{}
	for rows.Next() {
		var r syncwire.Revision
		e := rows.Scan(&r.ID, &r.Object, &r.Base, &r.Metadata, &r.FileKey)
		if e != nil {
			rows.Close()
			return nil, ErrStore
		}
		records = append(records, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, ErrStore
	}
	revisions := []Revision{}
	parents := map[string]string{}
	files := map[string]uint64{}
	for _, r := range records {
		if _, e = parseID(r.ID); e != nil {
			return nil, e
		}
		if r.Base != "" && parents[r.Base] != r.Object {
			return nil, ErrInvalid
		}
		parents[r.ID] = r.Object
		m, file, key, e := s.unlock(master, r)
		if e != nil {
			vc.Wipe(key[:])
			return nil, e
		}
		if m.Type == "file" {
			var count uint64
			if tx.QueryRow(`SELECT COUNT(*) FROM chunks WHERE file=?`, file.String()).Scan(&count) != nil || count != m.ChunkCount {
				vc.Wipe(key[:])
				return nil, ErrInvalid
			}
			left := m.Size
			for i := uint64(0); i < count; i++ {
				var data []byte
				if tx.QueryRow(`SELECT data FROM chunks WHERE file=? AND idx=?`, file.String(), i).Scan(&data) != nil {
					vc.Wipe(key[:])
					return nil, ErrInvalid
				}
				plain, err := vc.Open(key, vc.Context{Kind: vc.Chunk, Account: s.account, Object: file, Index: i, Final: i == count-1}, data, nil)
				length := uint64(len(plain))
				vc.Wipe(plain)
				if err != nil || length != min(uint64(vc.ChunkSize), left) {
					vc.Wipe(key[:])
					return nil, ErrInvalid
				}
				left -= length
			}
			files[file.String()] = count
		}
		vc.Wipe(key[:])
		revisions = append(revisions, Revision{Head: Head{Object: r.Object, Revision: r.ID, Metadata: m}, Base: r.Base})
	}
	var total, expected uint64
	for _, count := range files {
		expected += count
	}
	if tx.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&total) != nil || total != expected {
		return nil, ErrInvalid
	}
	return revisions, nil
}
