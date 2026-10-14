package sync_test

import (
	"context"
	"testing"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
)

func TestFolderIconEditsAndEncryptedSync(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "icon-a", true)
	b, cb := h.device(t, "icon-b", false)
	object, _ := vc.RandomID()
	revision, e := a.Put(h.master, object.String(), "", vc.Metadata{Type: "folder", Name: "Public folder", Tags: []string{}, FolderIcon: "1f393"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, icon := range []*string{nil, ptrIcon("1f4bc"), ptrIcon("")} {
		revision, e = a.Edit(h.master, revision, vs.Edit{Name: "Renamed folder", Tags: []string{}, Favorite: true, FolderIcon: icon})
		if e != nil {
			t.Fatal(e)
		}
		if e = ca.Sync(context.Background()); e != nil {
			t.Fatal(e)
		}
		if e = cb.Sync(context.Background()); e != nil {
			t.Fatal(e)
		}
		got, e := b.Revision(h.master, revision)
		want := "1f393"
		if icon != nil {
			want = *icon
		}
		if e != nil || got.Metadata.FolderIcon != want {
			t.Fatalf("icon lost: want %q, got %q: %v", want, got.Metadata.FolderIcon, e)
		}
	}
}

func ptrIcon(value string) *string { return &value }
