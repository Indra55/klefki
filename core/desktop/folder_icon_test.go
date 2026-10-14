package desktop

import (
	vs "klefki/core/sync"
	"testing"
)

func TestFolderIconLifecycle(t *testing.T) {
	s, _ := testService(t)
	revision, e := s.FolderWithIcon("Public folder", nil, "1f393")
	if e != nil {
		t.Fatal(e)
	}
	if find(t, s, revision).FolderIcon != "1f393" {
		t.Fatal("missing folder icon")
	}
	revision, e = s.Edit(revision, vs.Edit{Name: "Renamed", Tags: []string{}, Favorite: true})
	if e != nil {
		t.Fatal(e)
	}
	revision, e = s.Trash(revision)
	if e != nil {
		t.Fatal(e)
	}
	restored, e := s.Restore(revision)
	if e != nil {
		t.Fatal(e)
	}
	if find(t, s, restored.Revision).FolderIcon != "1f393" {
		t.Fatal("icon lost through edit or trash")
	}
	if _, e = s.FolderWithIcon("Invalid", nil, "../invalid"); e == nil {
		t.Fatal("invalid icon accepted")
	}
	file := put(t, s, "public.txt", nil)
	icon := "1f393"
	if _, e = s.Edit(file, vs.Edit{Name: "public.txt", Tags: []string{}, FolderIcon: &icon}); e == nil {
		t.Fatal("file icon accepted")
	}
}
