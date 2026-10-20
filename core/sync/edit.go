package sync

import (
	vc "klefki/core/crypto"
	"reflect"
	"slices"
)

type Edit struct {
	Name       string      `json:"name"`
	Parent     *string     `json:"parent"`
	Tags       []string    `json:"tags"`
	Favorite   bool        `json:"favorite"`
	Trash      *TrashState `json:"-"`
	FolderIcon *string     `json:"folder_icon,omitempty"`
}
type Revision struct {
	Head
	Base           string
	ContentMissing bool
}

func (s *Store) Revision(master vc.Key, id string) (Revision, error) {
	defer vc.Wipe(master[:])
	r, e := readRevision(s.db.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, id))
	if e != nil {
		return Revision{}, ErrStore
	}
	m, _, key, e := s.unlock(master, r)
	vc.Wipe(key[:])
	if e != nil {
		return Revision{}, e
	}
	return Revision{Head: Head{Object: r.Object, Revision: r.ID, Metadata: m}, Base: r.Base}, nil
}

// Edit publishes only new metadata; content and its authenticated key binding stay intact.
func (s *Store) Edit(master vc.Key, base string, edit Edit) (string, error) {
	defer vc.Wipe(master[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return "", ErrStore
	}
	defer tx.Rollback()
	var children int
	if tx.QueryRow(`SELECT COUNT(*) FROM revisions WHERE base=?`, base).Scan(&children) != nil {
		return "", ErrStore
	}
	if children != 0 {
		return "", ErrConflict
	}
	r, e := readRevision(tx.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, base))
	if e != nil {
		return "", ErrConflict
	}
	m, _, key, e := s.unlock(master, r)
	vc.Wipe(key[:])
	if e != nil {
		return "", e
	}
	unchanged := m.Name == edit.Name && reflect.DeepEqual(m.Parent, edit.Parent) && slices.Equal(m.Tags, edit.Tags) && m.Favorite == edit.Favorite && (edit.Trash == nil || m.TrashedAt == edit.Trash.At && reflect.DeepEqual(m.TrashPreviousParent, edit.Trash.PreviousParent)) && (edit.FolderIcon == nil || m.FolderIcon == *edit.FolderIcon)
	if unchanged {
		return base, nil
	}
	if edit.Trash != nil {
		m.TrashedAt = edit.Trash.At
		m.TrashPreviousParent = edit.Trash.PreviousParent
	}
	if edit.FolderIcon != nil {
		if m.Type != "folder" || !vc.ValidFolderIcon(*edit.FolderIcon) {
			return "", vc.ErrInvalid
		}
		m.FolderIcon = *edit.FolderIcon
	}
	if edit.Parent != nil {
		if deleted, e := deletedObject(tx, *edit.Parent); e != nil {
			return "", e
		} else if deleted {
			return "", ErrConflict
		}
	}
	m.Name = edit.Name
	m.Parent = edit.Parent
	m.Tags = edit.Tags
	m.Favorite = edit.Favorite
	oid, e := vc.ParseID(r.Object)
	if e != nil {
		return "", e
	}
	data, e := vc.EncryptMetadata(master, s.account, oid, m)
	if e != nil {
		return "", e
	}
	id, e := vc.RandomID()
	if e != nil {
		return "", e
	}
	if _, e = tx.Exec(`INSERT INTO revisions(id,object,base,metadata,file_key,pending) VALUES(?,?,?,?,?,1)`, id.String(), r.Object, base, data, r.FileKey); e != nil {
		return "", ErrStore
	}
	if _, e = tx.Exec(`INSERT OR IGNORE INTO deferred_content(revision,wanted,trash) SELECT ?,wanted,trash FROM deferred_content WHERE revision=?`, id.String(), base); e != nil {
		return "", ErrStore
	}
	if tx.Commit() != nil {
		return "", ErrStore
	}
	return id.String(), nil
}

func (s *Store) BindRemote(address string) error {
	r, e := NewRemote(address, nil, false)
	if e != nil {
		return e
	}
	return s.bind(r.base)
}
