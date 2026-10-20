package desktop

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
)

func TestTransferMixedItemsAndIndependentRecursiveCopy(t *testing.T) {
	s, _ := testService(t)
	folder, _ := s.FolderWithIcon("Records", nil, "1f393")
	f := find(t, s, folder)
	nested, _ := s.Folder("Nested", &f.Object)
	n := find(t, s, nested)
	child := put(t, s, "inside.txt", &n.Object)
	loose := put(t, s, "loose.txt", nil)
	target, _ := s.Folder("Destination", nil)
	d := find(t, s, target)
	if e := s.Transfer([]string{folder, loose}, &d.Object, true); e != nil {
		t.Fatal(e)
	}
	list, _ := s.List()
	if len(list.Items) != 9 {
		t.Fatalf("copy has %d items, want 9", len(list.Items))
	}
	var copiedFolder, copiedNested, copiedFile Item
	for _, item := range list.Items {
		if item.Name == f.Name && sameParent(item.Parent, &d.Object) {
			copiedFolder = item
		}
	}
	if copiedFolder.Object == "" || copiedFolder.Object == f.Object || copiedFolder.FolderIcon != f.FolderIcon {
		t.Fatal("folder identity or icon lost")
	}
	for _, item := range list.Items {
		if item.Name == n.Name && sameParent(item.Parent, &copiedFolder.Object) {
			copiedNested = item
		}
	}
	for _, item := range list.Items {
		if item.Name == "inside.txt" && sameParent(item.Parent, &copiedNested.Object) {
			copiedFile = item
		}
	}
	if copiedFile.Object == "" {
		t.Fatal("nested copy missing")
	}
	if e := s.with(func(x *session) error {
		original, _ := x.store.Revision(x.master, child)
		copy, _ := x.store.Revision(x.master, copiedFile.Revision)
		if original.Metadata.FileID == copy.Metadata.FileID || !reflect.DeepEqual(original.Metadata.Tags, copy.Metadata.Tags) {
			t.Fatal("copy shares content identity or loses tags")
		}
		var out bytes.Buffer
		if e := x.store.Export(x.master, copiedFile.Revision, &out); e != nil {
			return e
		}
		if out.String() != "private-body" {
			t.Fatal("copy content changed")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.Transfer([]string{folder, loose}, &d.Object, false); e != nil {
		t.Fatal(e)
	}
	list, _ = s.List()
	if len(list.Items) != 9 {
		t.Fatal("move duplicated items")
	}
	for _, item := range list.Items {
		if item.Object == f.Object && !sameParent(item.Parent, &d.Object) {
			t.Fatal("folder did not move")
		}
	}
	if find(t, s, child).Parent == nil || *find(t, s, child).Parent != n.Object {
		t.Fatal("move rewrote descendants")
	}
	trashed, e := s.Trash(copiedFolder.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if find(t, s, child).Trashed || !find(t, s, trashed).Trashed {
		t.Fatal("copy trash affected original")
	}
}

func TestTransferRejectsInvalidSelectionsAndStaleSnapshots(t *testing.T) {
	s, _ := testService(t)
	folder, _ := s.Folder("Folder", nil)
	f := find(t, s, folder)
	nested, _ := s.Folder("Nested", &f.Object)
	n := find(t, s, nested)
	loose := put(t, s, "loose.txt", nil)
	looseObject := find(t, s, loose).Object
	inside := put(t, s, "inside.txt", &f.Object)
	for _, copy := range []bool{false, true} {
		for _, tc := range []struct {
			ids    []string
			parent *string
		}{{nil, nil}, {[]string{folder}, &f.Object}, {[]string{folder}, &n.Object}, {[]string{folder, inside}, nil}, {[]string{loose, loose}, &f.Object}, {[]string{loose}, nil}, {[]string{loose}, &looseObject}} {
			if e := s.Transfer(tc.ids, tc.parent, copy); e == nil {
				t.Fatalf("accepted invalid selection, copy=%v", copy)
			}
		}
	}
	if e := s.with(func(x *session) error {
		snapshot, e := x.store.AllHeads(x.master)
		if e != nil {
			return e
		}
		if _, e = x.store.Edit(x.master, loose, vs.Edit{Name: "renamed.txt", Tags: []string{}}); e != nil {
			return e
		}
		return x.store.TransferContext(context.Background(), x.master, snapshot, []vs.TransferChange{{Revision: folder, Parent: &n.Object}}, false)
	}); !errors.Is(e, vs.ErrConflict) {
		t.Fatal("stale snapshot accepted", e)
	}
	if find(t, s, folder).Parent != nil {
		t.Fatal("stale transfer changed source")
	}
}

func TestTransferRollsBackEarlierCopiesOnFailure(t *testing.T) {
	s, _ := testService(t)
	first, _ := s.Folder("First", nil)
	file := put(t, s, "file.txt", nil)
	target, _ := s.Folder("Destination", nil)
	d := find(t, s, target)
	before, _ := s.List()
	e := s.with(func(x *session) error {
		snapshot, e := x.store.AllHeads(x.master)
		if e != nil {
			return e
		}
		id, _ := vc.RandomID()
		return x.store.TransferContext(x.ctx, x.master, snapshot, []vs.TransferChange{{Revision: first, Object: id.String(), Parent: &d.Object}, {Revision: file, Object: "invalid", Parent: &d.Object}}, true)
	})
	if e == nil {
		t.Fatal("invalid copy accepted")
	}
	after, _ := s.List()
	if !reflect.DeepEqual(before.Items, after.Items) {
		t.Fatal("failed copy left partial revisions")
	}
	if e = s.Transfer([]string{first, file}, &d.Object, true); e != nil {
		t.Fatal("failed transaction left unusable store", e)
	}
}
