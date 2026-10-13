package sync

import (
	"slices"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

const TrashRetention = 30 * 24 * time.Hour
const trashTag = "klefki:system:trash"

type TrashState struct {
	At             int64
	PreviousParent *string
}
type DeletionPreview struct {
	Revision string              `json:"revision"`
	Count    int                 `json:"count"`
	Targets  []syncwire.Deletion `json:"targets"`
}

func trashRoot(m vc.Metadata) bool { return m.Type == "folder" && slices.Contains(m.Tags, trashTag) }

// TrashDates returns the inherited trash date of each visible revision.
func TrashDates(heads []Head) map[string]int64 {
	byObject := map[string][]Head{}
	for _, h := range heads {
		byObject[h.Object] = append(byObject[h.Object], h)
	}
	var date func(Head, map[string]bool) int64
	date = func(h Head, seen map[string]bool) int64 {
		if seen[h.Object] || h.Metadata.Parent == nil {
			return 0
		}
		seen[h.Object] = true
		for _, p := range byObject[*h.Metadata.Parent] {
			if trashRoot(p.Metadata) {
				return h.Metadata.TrashedAt
			}
			if at := date(p, seen); at > 0 {
				return at
			}
		}
		return 0
	}
	out := map[string]int64{}
	for _, h := range heads {
		out[h.Revision] = date(h, map[string]bool{})
	}
	return out
}
func (s *Store) InitializeTrash(master vc.Key, now time.Time) error {
	defer vc.Wipe(master[:])
	heads, e := s.Heads(master)
	if e != nil {
		return e
	}
	roots := map[string]bool{}
	for _, h := range heads {
		if trashRoot(h.Metadata) {
			roots[h.Object] = true
		}
	}
	for _, h := range heads {
		m := h.Metadata
		if h.Conflict || m.Parent == nil || !roots[*m.Parent] || m.TrashedAt != 0 || trashRoot(m) {
			continue
		}
		previous := m.TrashPreviousParent
		r, e := s.Revision(master, h.Revision)
		if e != nil {
			return e
		}
		if r.Base != "" {
			old, e := s.Revision(master, r.Base)
			if e != nil {
				return e
			}
			previous = old.Metadata.Parent
		}
		if _, e = s.Edit(master, h.Revision, Edit{Name: m.Name, Parent: m.Parent, Tags: m.Tags, Favorite: m.Favorite, Trash: &TrashState{now.UnixMilli(), previous}}); e != nil {
			return e
		}
	}
	return nil
}
func previewDeletion(heads []Head, revision string) (DeletionPreview, error) {
	out := DeletionPreview{Revision: revision, Targets: []syncwire.Deletion{}}
	byObject := map[string][]Head{}
	roots := map[string]bool{}
	for _, h := range heads {
		byObject[h.Object] = append(byObject[h.Object], h)
		if trashRoot(h.Metadata) {
			roots[h.Object] = true
		}
	}
	var trashed func(Head, map[string]bool) bool
	trashed = func(h Head, seen map[string]bool) bool {
		if h.Metadata.Parent == nil || seen[h.Object] {
			return false
		}
		seen[h.Object] = true
		if roots[*h.Metadata.Parent] {
			return true
		}
		for _, p := range byObject[*h.Metadata.Parent] {
			if trashed(p, seen) {
				return true
			}
		}
		return false
	}
	selected := map[string]bool{}
	for _, h := range heads {
		if (revision == "" || h.Revision == revision) && !trashRoot(h.Metadata) && !internalMetadata(h.Metadata) && trashed(h, map[string]bool{}) {
			selected[h.Object] = true
		}
	}
	if revision != "" && len(selected) == 0 {
		return out, ErrConflict
	}
	for changed := true; changed; {
		changed = false
		for _, h := range heads {
			if !selected[h.Object] && h.Metadata.Parent != nil && selected[*h.Metadata.Parent] {
				selected[h.Object] = true
				changed = true
			}
		}
	}
	for object := range selected {
		d := syncwire.Deletion{Object: object, Files: []string{}, Heads: []string{}}
		for _, h := range byObject[object] {
			d.Folder = d.Folder || h.Metadata.Type == "folder"
			d.Heads = append(d.Heads, h.Revision)
		}
		slices.Sort(d.Heads)
		out.Targets = append(out.Targets, d)
	}
	slices.SortFunc(out.Targets, func(a, b syncwire.Deletion) int { return compareStrings(a.Object, b.Object) })
	out.Count = len(out.Targets)
	return out, nil
}
func compareStrings(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Children precede folders so a stale batch leaves its ancestors recoverable.
func OrderDeletionTargets(heads []Head, targets []syncwire.Deletion) []syncwire.Deletion {
	parents := map[string]*string{}
	for _, h := range heads {
		parents[h.Object] = h.Metadata.Parent
	}
	depth := func(object string) int {
		seen := map[string]bool{}
		n := 0
		for parents[object] != nil && !seen[object] {
			seen[object] = true
			object = *parents[object]
			n++
		}
		return n
	}
	out := slices.Clone(targets)
	slices.SortFunc(out, func(a, b syncwire.Deletion) int {
		if d := depth(b.Object) - depth(a.Object); d != 0 {
			return d
		}
		return compareStrings(a.Object, b.Object)
	})
	return out
}
func (s *Store) PreviewDeletion(master vc.Key, revision string) (DeletionPreview, error) {
	defer vc.Wipe(master[:])
	heads, e := s.Heads(master)
	if e != nil {
		return DeletionPreview{}, e
	}
	return previewDeletion(heads, revision)
}
func sameTargets(a, b DeletionPreview) bool {
	if len(a.Targets) != len(b.Targets) {
		return false
	}
	for i, x := range a.Targets {
		y := b.Targets[i]
		if x.Object != y.Object || x.Folder != y.Folder || !slices.Equal(x.Heads, y.Heads) {
			return false
		}
	}
	return true
}
func (s *Store) DeleteTrash(master vc.Key, preview DeletionPreview) error {
	defer vc.Wipe(master[:])
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	return s.deleteTrash(master, preview)
}
func (s *Store) deleteTrash(master vc.Key, preview DeletionPreview) error {
	s.mu.Lock()
	current, e := s.PreviewDeletion(master, preview.Revision)
	if e != nil {
		s.mu.Unlock()
		return e
	}
	if !sameTargets(current, preview) {
		s.mu.Unlock()
		return ErrConflict
	}
	if s.Bound() && !s.DeletionSupported() {
		s.mu.Unlock()
		return ErrDeletionUnsupported
	}

	e = s.eraseLocked(current.Targets, true)
	s.mu.Unlock()
	if e != nil {
		return e
	}
	if e = s.PruneDeleted(master); e != nil {
		return e
	}
	return s.Compact()
}

// ExpiredTrash skips a whole subtree if any of its objects has unresolved heads.
func (s *Store) ExpiredTrash(master vc.Key, now time.Time) ([]DeletionPreview, error) {
	defer vc.Wipe(master[:])
	heads, e := s.Heads(master)
	if e != nil {
		return nil, e
	}
	byObject := map[string][]Head{}
	for _, h := range heads {
		byObject[h.Object] = append(byObject[h.Object], h)
	}
	var conflicted func(Head, map[string]bool) bool
	conflicted = func(h Head, seen map[string]bool) bool {
		if seen[h.Object] {
			return true
		}
		seen[h.Object] = true
		if h.Conflict {
			return true
		}
		if h.Metadata.Parent != nil {
			for _, p := range byObject[*h.Metadata.Parent] {
				if conflicted(p, seen) {
					return true
				}
			}
		}
		return false
	}
	dates := TrashDates(heads)
	out := []DeletionPreview{}
	covered := map[string]bool{}
	for _, h := range heads {
		at := dates[h.Revision]
		if at <= 0 || now.UnixMilli()-at < TrashRetention.Milliseconds() || covered[h.Object] || internalMetadata(h.Metadata) || conflicted(h, map[string]bool{}) {
			continue
		}
		p, e := previewDeletion(heads, h.Revision)
		if e != nil {
			return nil, e
		}
		conflict := false
		for _, d := range p.Targets {
			if len(d.Heads) != 1 {
				conflict = true
			}
		}
		if conflict {
			for _, d := range p.Targets {
				covered[d.Object] = true
			}
			continue
		}
		out = append(out, p)
		for _, d := range p.Targets {
			covered[d.Object] = true
		}
	}
	return out, nil
}
func (s *Store) CleanupLocalTrash(master vc.Key, now time.Time) error {
	defer vc.Wipe(master[:])
	if s.Bound() {
		return s.PruneDeleted(master)
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if e := s.PruneDeleted(master); e != nil {
		return e
	}
	if e := s.InitializeTrash(master, now); e != nil {
		return e
	}
	expired, e := s.ExpiredTrash(master, now)
	if e != nil {
		return e
	}
	for _, p := range expired {
		if e = s.deleteTrash(master, p); e != nil {
			return e
		}
	}
	var compact int
	if e = s.db.QueryRow(`SELECT compact FROM deletion_state WHERE id=1`).Scan(&compact); e != nil {
		return e
	}
	if compact == 1 {
		return s.Compact()
	}
	return nil
}
