package sync_test

import (
	"bytes"
	"context"
	"testing"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
)

func TestTransferCopiesSyncWithIndependentContent(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "transfer-a", true)
	b, cb := h.device(t, "transfer-b", false)
	folder, _ := vc.RandomID()
	file, _ := vc.RandomID()
	copyFolder, _ := vc.RandomID()
	copyFile, _ := vc.RandomID()
	parent := folder.String()
	copyParent := copyFolder.String()
	folderRevision, e := a.Put(h.master, parent, "", vc.Metadata{Type: "folder", Name: "Records", Tags: []string{"Work"}, Favorite: true, FolderIcon: "1f393"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	body := bytes.Repeat([]byte("independent encrypted copy\n"), 50000)
	fileRevision, e := a.Put(h.master, file.String(), "", vc.Metadata{Type: "file", Name: "large.txt", Parent: &parent, Tags: []string{"Work"}, Size: uint64(len(body))}, bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := a.AllHeads(h.master)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.TransferContext(context.Background(), h.master, snapshot, []vs.TransferChange{{Revision: folderRevision, Object: copyParent}, {Revision: fileRevision, Object: copyFile.String(), Parent: &copyParent}}, true); e != nil {
		t.Fatal(e)
	}
	if e = ca.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = cb.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
	heads, e := b.AllHeads(h.master)
	if e != nil {
		t.Fatal(e)
	}
	if len(heads) != 4 {
		t.Fatalf("received %d heads, want 4", len(heads))
	}
	for _, head := range heads {
		if head.Object == copyParent && (head.Metadata.FolderIcon != "1f393" || !head.Metadata.Favorite) {
			t.Fatal("folder metadata lost")
		}
		if head.Object != copyFile.String() {
			continue
		}
		original, _ := a.Revision(h.master, fileRevision)
		if head.Metadata.FileID == original.Metadata.FileID {
			t.Fatal("copy reuses original file identity")
		}
		var out bytes.Buffer
		if e = b.Export(h.master, head.Revision, &out); e != nil || !bytes.Equal(out.Bytes(), body) {
			t.Fatal("synced copy content differs", e)
		}
	}
}
