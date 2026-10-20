package desktop

import (
	vc "klefki/core/crypto"
	"klefki/core/search"
	vs "klefki/core/sync"
)

func (s *Service) Transfer(revisions []string, parent *string, duplicate bool) error {
	if len(revisions) == 0 {
		return ErrInvalid
	}
	return s.with(func(x *session) error {
		heads, err := x.store.AllHeads(x.master)
		if err != nil {
			return err
		}
		list := items(heads)
		byRevision := map[string]Item{}
		for _, item := range list {
			byRevision[item.Revision] = item
		}
		selected := []Item{}
		seen := map[string]bool{}
		var source *string
		for n, revision := range revisions {
			item, ok := byRevision[revision]
			if !ok {
				return vs.ErrConflict
			}
			if item.Trashed || item.Conflict || seen[item.Object] || !validParent(heads, item.Object, parent) {
				return ErrInvalid
			}
			if n == 0 {
				source = item.Parent
			} else if !sameParent(source, item.Parent) {
				return ErrInvalid
			}
			seen[item.Object] = true
			selected = append(selected, item)
		}
		if sameParent(source, parent) {
			return ErrInvalid
		}
		if duplicate {
			for n := 0; n < len(selected); n++ {
				if selected[n].Type != "folder" {
					continue
				}
				for _, head := range heads {
					if head.Metadata.Parent == nil || *head.Metadata.Parent != selected[n].Object {
						continue
					}
					item, ok := byRevision[head.Revision]
					if !ok || item.Trashed || item.Conflict || seen[item.Object] || search.IsInternal(head.Metadata) {
						return ErrInvalid
					}
					seen[item.Object] = true
					selected = append(selected, item)
				}
			}
		}
		objects := map[string]string{}
		changes := make([]vs.TransferChange, 0, len(selected))
		for n, item := range selected {
			object := item.Object
			if duplicate {
				id, e := vc.RandomID()
				if e != nil {
					return e
				}
				object = id.String()
			}
			objects[item.Object] = object
			destination := parent
			if n >= len(revisions) {
				mapped := objects[*item.Parent]
				destination = &mapped
			}
			changes = append(changes, vs.TransferChange{Revision: item.Revision, Object: object, Parent: destination})
		}
		if err = x.store.TransferContext(x.ctx, x.master, heads, changes, duplicate); err != nil {
			return err
		}
		s.changed(x)
		return nil
	})
}

func sameParent(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
