package sync

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	vc "klefki/core/crypto"
)

type TagChange struct {
	Revision string `json:"revision"`
	Tag      string `json:"tag"`
}

func ValidUserTag(tag string) bool {
	return tag != "" && tag == strings.TrimSpace(tag) && len(tag) <= 80 && utf8.ValidString(tag) &&
		!strings.HasPrefix(strings.ToLower(tag), "klefki:system:") && strings.IndexFunc(tag, unicode.IsControl) < 0
}

// TagFiles adds reviewed tags in one transaction and retains the encrypted content.
func (s *Store) TagFiles(ctx context.Context, master vc.Key, changes []TagChange) (int, error) {
	defer vc.Wipe(master[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, ErrStore
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	count := 0
	for _, change := range changes {
		if e := ctx.Err(); e != nil {
			return 0, e
		}
		if !ValidUserTag(change.Tag) || seen[change.Revision] {
			return 0, vc.ErrInvalid
		}
		seen[change.Revision] = true
		var children int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM revisions WHERE base=?`, change.Revision).Scan(&children) != nil {
			return 0, ErrStore
		}
		if children != 0 {
			return 0, ErrConflict
		}
		r, e := readRevision(tx.QueryRowContext(ctx, `SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, change.Revision))
		if e != nil {
			return 0, ErrConflict
		}
		m, _, key, e := s.unlock(master, r)
		vc.Wipe(key[:])
		if e != nil {
			return 0, e
		}
		if m.Type != "file" {
			return 0, vc.ErrInvalid
		}
		present := false
		for _, tag := range m.Tags {
			if strings.HasPrefix(tag, "klefki:system:") {
				return 0, vc.ErrInvalid
			}
			present = present || strings.EqualFold(tag, change.Tag)
		}
		if present {
			continue
		}
		m.Tags = append(m.Tags, change.Tag)
		oid, e := vc.ParseID(r.Object)
		if e != nil {
			return 0, e
		}
		data, e := vc.EncryptMetadata(master, s.account, oid, m)
		if e != nil {
			return 0, e
		}
		id, e := vc.RandomID()
		if e != nil {
			return 0, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO revisions(id,object,base,metadata,file_key,pending) VALUES(?,?,?,?,?,1)`, id.String(), r.Object, r.ID, data, r.FileKey); e != nil {
			return 0, ErrStore
		}
		count++
	}
	if e := tx.Commit(); e != nil {
		return 0, ErrStore
	}
	return count, nil
}
