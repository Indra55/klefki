package desktop

import (
	"context"
	"strings"
	"time"
	vc "vault/core/crypto"
	vs "vault/core/sync"
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
		if strings.TrimSpace(t) == "" || len(t) > 80 || strings.HasPrefix(t, "vault:system:") {
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

type Restored struct {
	Revision string `json:"revision"`
	AtRoot   bool   `json:"atRoot"`
}

func check(ctx context.Context) error {
	if ctx.Err() != nil {
		return context.Canceled
	}
	return nil
}
