package sync

import (
	"context"

	vc "klefki/core/crypto"
)

type TransferChange struct {
	Revision string
	Object   string
	Parent   *string
}

// TransferContext commits a reviewed snapshot as one batch, including copied chunks.
func (s *Store) TransferContext(ctx context.Context, master vc.Key, expected []Head, changes []TransferChange, duplicate bool) error {
	defer vc.Wipe(master[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return ErrStore
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT r.id FROM revisions r WHERE NOT EXISTS(SELECT 1 FROM revisions child WHERE child.base=r.id)`)
	if err != nil {
		return ErrStore
	}
	snapshot := map[string]bool{}
	for _, h := range expected {
		snapshot[h.Revision] = true
	}
	count := 0
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return ErrStore
		}
		if !snapshot[id] {
			rows.Close()
			return ErrConflict
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrStore
	}
	if count != len(snapshot) {
		return ErrConflict
	}
	for _, change := range changes {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !snapshot[change.Revision] {
			return ErrConflict
		}
		r, e := readRevision(tx.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, change.Revision))
		if e != nil {
			return ErrStore
		}
		m, file, key, e := s.unlock(master, r)
		if e != nil {
			return e
		}
		vc.Wipe(key[:])
		m.Parent = change.Parent
		base := r.ID
		object := r.Object
		wrapped := r.FileKey
		if duplicate {
			base = ""
			object = change.Object
			if m.Type == "file" {
				_, _, oldKey, e := s.unlock(master, r)
				if e != nil {
					return e
				}
				newFile, e := vc.RandomID()
				if e != nil {
					vc.Wipe(oldKey[:])
					return e
				}
				newKey, e := vc.RandomKey()
				if e != nil {
					vc.Wipe(oldKey[:])
					return e
				}
				wrapped, e = vc.Seal(master, vc.Context{Kind: vc.FileKey, Account: s.account, Object: newFile}, newKey[:], nil)
				if e == nil {
					var size uint64
					for i := uint64(0); i < m.ChunkCount; i++ {
						if e = ctx.Err(); e != nil {
							break
						}
						var encrypted []byte
						if tx.QueryRow(`SELECT data FROM chunks WHERE file=? AND idx=?`, file.String(), i).Scan(&encrypted) != nil {
							e = ErrContentUnavailable
							break
						}
						var plain []byte
						plain, e = vc.Open(oldKey, vc.Context{Kind: vc.Chunk, Account: s.account, Object: file, Index: i, Final: i == m.ChunkCount-1}, encrypted, nil)
						if e != nil {
							break
						}
						size += uint64(len(plain))
						encrypted, e = vc.Seal(newKey, vc.Context{Kind: vc.Chunk, Account: s.account, Object: newFile, Index: i, Final: i == m.ChunkCount-1}, plain, nil)
						vc.Wipe(plain)
						if e != nil {
							break
						}
						if _, e = tx.Exec(`INSERT INTO chunks VALUES(?,?,?)`, newFile.String(), i, encrypted); e != nil {
							e = ErrStore
							break
						}
					}
					if e == nil && size != m.Size {
						e = ErrInvalid
					}
				}
				vc.Wipe(oldKey[:])
				vc.Wipe(newKey[:])
				if e != nil {
					return e
				}
				m.FileID = newFile.String()
			}
		}
		oid, e := vc.ParseID(object)
		if e != nil {
			return e
		}
		metadata, e := vc.EncryptMetadata(master, s.account, oid, m)
		if e != nil {
			return e
		}
		id, e := vc.RandomID()
		if e != nil {
			return e
		}
		if _, e = tx.Exec(`INSERT INTO revisions(id,object,base,metadata,file_key,pending) VALUES(?,?,?,?,?,1)`, id.String(), object, base, metadata, wrapped); e != nil {
			return ErrStore
		}
		if !duplicate {
			if _, e = tx.Exec(`INSERT OR IGNORE INTO deferred_content(revision,wanted,trash) SELECT ?,wanted,trash FROM deferred_content WHERE revision=?`, id.String(), r.ID); e != nil {
				return ErrStore
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return ErrStore
	}
	return nil
}
