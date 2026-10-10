package desktop

import (
	"context"
	"strings"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/search"
	vs "klefki/core/sync"
)

type Item struct {
	Object    string   `json:"object"`
	Revision  string   `json:"revision"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Parent    *string  `json:"parent"`
	Tags      []string `json:"tags"`
	Favorite  bool     `json:"favorite"`
	Size      uint64   `json:"size"`
	Conflict  bool     `json:"conflict"`
	Trashed   bool     `json:"trashed"`
	TrashRoot bool     `json:"trashRoot"`
	Category  string   `json:"category"`
}
type Library struct {
	Generation uint64 `json:"generation"`
	Items      []Item `json:"items"`
}

func system(m vc.Metadata) bool {
	for _, t := range m.Tags {
		if t == TrashTag && m.Type == "folder" {
			return true
		}
	}
	return false
}
func items(heads []vs.Head) []Item {
	byObject := map[string][]vs.Head{}
	for _, h := range heads {
		byObject[h.Object] = append(byObject[h.Object], h)
	}
	var trashed func(vs.Head, map[string]bool) bool
	trashed = func(h vs.Head, seen map[string]bool) bool {
		if system(h.Metadata) {
			return true
		}
		if seen[h.Object] || h.Metadata.Parent == nil {
			return false
		}
		seen[h.Object] = true
		for _, p := range byObject[*h.Metadata.Parent] {
			if trashed(p, seen) {
				return true
			}
		}
		return false
	}
	out := make([]Item, 0, len(heads))
	for _, h := range heads {
		m := h.Metadata
		if search.IsInternal(m) {
			continue
		}
		out = append(out, Item{h.Object, h.Revision, m.Name, m.Type, m.Parent, m.Tags, m.Favorite, m.Size, h.Conflict, trashed(h, map[string]bool{}), system(m), ""})
	}
	return out
}
func (s *Service) List() (out Library, e error) {
	e = s.with(func(x *session) error {
		h, e := x.store.Heads(x.master)
		if e != nil {
			return e
		}
		list := items(h)
		if s.semanticPreferences(x).Enabled {
			cs, e := search.Corrections(x.ctx, x.store, x.master, h)
			if e != nil {
				return e
			}
			for i := range list {
				for _, head := range h {
					if head.Revision != list[i].Revision {
						continue
					}
					matches := []search.CorrectionRecord{}
					for _, r := range cs {
						if r.Correction.Object == head.Object && r.Correction.FileID == head.Metadata.FileID {
							matches = append(matches, r)
						}
					}
					if len(matches) == 1 {
						list[i].Category = matches[0].Correction.Category
					}
				}
			}
		}
		out = Library{x.generation, list}
		return nil
	})
	return
}
func validEdit(e vs.Edit) bool {
	if strings.TrimSpace(e.Name) == "" || len(e.Name) > 255 || strings.ContainsAny(e.Name, "\x00/\\") || len(e.Tags) > 64 {
		return false
	}
	for _, t := range e.Tags {
		if strings.TrimSpace(t) == "" || len(t) > 80 || strings.HasPrefix(t, "klefki:system:") {
			return false
		}
	}
	return true
}
func validParent(heads []vs.Head, object string, parent *string) bool {
	seen := map[string]bool{object: true}
	list := items(heads)
	for parent != nil {
		if seen[*parent] {
			return false
		}
		seen[*parent] = true
		var match *Item
		for i := range list {
			if list[i].Object == *parent {
				if match != nil {
					return false
				}
				match = &list[i]
			}
		}
		if match == nil || match.Type != "folder" || match.Trashed {
			return false
		}
		parent = match.Parent
	}
	return true
}
func (s *Service) changed(x *session) {
	n, _ := x.store.Pending()
	s.mu.Lock()
	if s.session == x {
		s.pending = n
		if s.config.Server != "" && !x.recovery {
			s.syncState = "Pending"
		}
	}
	s.mu.Unlock()
	s.event(Event{Generation: x.generation, Kind: "library"})
	go func() {
		select {
		case <-timeAfterDebounce():
			s.SyncNow()
		case <-x.ctx.Done():
		}
	}()
}
func timeAfterDebounce() <-chan time.Time { return time.After(1500 * time.Millisecond) }
func (s *Service) Edit(revision string, edit vs.Edit) (id string, e error) {
	if !validEdit(edit) {
		return "", ErrInvalid
	}
	e = s.with(func(x *session) error {
		r, e := x.store.Revision(x.master, revision)
		if e != nil {
			return e
		}
		h, e := x.store.Heads(x.master)
		if e != nil {
			return e
		}
		if system(r.Metadata) || !validParent(h, r.Object, edit.Parent) {
			return ErrInvalid
		}
		for _, it := range items(h) {
			if it.Revision == revision && it.Trashed {
				return ErrInvalid
			}
		}
		id, e = x.store.Edit(x.master, revision, edit)
		if e == nil {
			s.changed(x)
		}
		return e
	})
	return
}
func (s *Service) Folder(name string, parent *string) (id string, e error) {
	if !validEdit(vs.Edit{Name: name, Tags: []string{}}) {
		return "", ErrInvalid
	}
	e = s.with(func(x *session) error {
		h, e := x.store.Heads(x.master)
		if e != nil {
			return e
		}
		obj, e := vc.RandomID()
		if e != nil {
			return e
		}
		if !validParent(h, obj.String(), parent) {
			return ErrInvalid
		}
		id, e = x.store.PutContext(x.ctx, x.master, obj.String(), "", vc.Metadata{Type: "folder", Name: name, Parent: parent, Tags: []string{}}, nil)
		if e == nil {
			s.changed(x)
		}
		return e
	})
	return
}
func (s *Service) Trash(revision string) (id string, e error) {
	e = s.with(func(x *session) error {
		r, e := x.store.Revision(x.master, revision)
		if e != nil {
			return e
		}
		h, e := x.store.Heads(x.master)
		if e != nil {
			return e
		}
		root := ""
		found := false
		for _, it := range items(h) {
			if it.TrashRoot && root == "" {
				root = it.Object
			}
			if it.Revision == revision {
				found = true
				if it.Trashed {
					return ErrInvalid
				}
			}
		}
		if !found {
			return vs.ErrConflict
		}
		if root == "" {
			obj, e := vc.RandomID()
			if e != nil {
				return e
			}
			root = obj.String()
			if _, e = x.store.PutContext(x.ctx, x.master, root, "", vc.Metadata{Type: "folder", Name: "Trash", Tags: []string{TrashTag}}, nil); e != nil {
				return e
			}
		}
		m := r.Metadata
		id, e = x.store.Edit(x.master, revision, vs.Edit{Name: m.Name, Parent: &root, Tags: m.Tags, Favorite: m.Favorite})
		if e == nil {
			s.changed(x)
		}
		return e
	})
	return
}

type Restored struct {
	Revision string `json:"revision"`
	AtRoot   bool   `json:"atRoot"`
}

func (s *Service) Restore(revision string) (out Restored, e error) {
	e = s.with(func(x *session) error {
		r, e := x.store.Revision(x.master, revision)
		if e != nil {
			return e
		}
		h, e := x.store.Heads(x.master)
		if e != nil {
			return e
		}
		list := items(h)
		found := false
		roots := map[string]bool{}
		for _, it := range list {
			if it.TrashRoot {
				roots[it.Object] = true
			}
			if it.Revision == revision && it.Trashed && !it.TrashRoot {
				found = true
			}
		}
		if !found {
			return ErrInvalid
		}
		m := r.Metadata
		var parent *string
		if m.Parent != nil && roots[*m.Parent] && r.Base != "" {
			previous, e := x.store.Revision(x.master, r.Base)
			if e != nil {
				return e
			}
			parent = previous.Metadata.Parent
		}
		if !validParent(h, r.Object, parent) {
			parent = nil
			out.AtRoot = true
		}
		if m.Parent != nil && !roots[*m.Parent] {
			out.AtRoot = true
		}
		out.Revision, e = x.store.Edit(x.master, revision, vs.Edit{Name: m.Name, Parent: parent, Tags: m.Tags, Favorite: m.Favorite})
		if e == nil {
			s.changed(x)
		}
		return e
	})
	return
}
func check(ctx context.Context) error {
	if ctx.Err() != nil {
		return context.Canceled
	}
	return nil
}
