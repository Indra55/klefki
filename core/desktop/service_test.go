package desktop

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	vc "vault/core/crypto"
	vs "vault/core/sync"
)

func testService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	s, e := New(filepath.Join(root, "app"), nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	code := filepath.Join(root, "recovery.txt")
	if e = s.Create("synthetic desktop password", code); e != nil {
		t.Fatal(e)
	}
	if e = s.Unlock("synthetic desktop password", false); e != nil {
		t.Fatal(e)
	}
	return s, code
}
func put(t *testing.T, s *Service, name string, parent *string) string {
	t.Helper()
	var rev string
	e := s.with(func(x *session) error {
		id, _ := vc.RandomID()
		var e error
		rev, e = x.store.Put(x.master, id.String(), "", vc.Metadata{Type: "file", Name: name, Parent: parent, Tags: []string{"private-desktop-tag"}, Size: 12}, strings.NewReader("private-body"))
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return rev
}
func find(t *testing.T, s *Service, rev string) Item {
	t.Helper()
	l, e := s.List()
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range l.Items {
		if i.Revision == rev {
			return i
		}
	}
	t.Fatal("missing revision")
	return Item{}
}
func TestTrashRestoreAndFolderCycle(t *testing.T) {
	s, _ := testService(t)
	folder, e := s.Folder("private-folder", nil)
	if e != nil {
		t.Fatal(e)
	}
	f := find(t, s, folder)
	rev := put(t, s, "private-document.txt", &f.Object)
	if _, e = s.Edit(folder, vs.Edit{Name: f.Name, Parent: &f.Object, Tags: []string{}}); e == nil {
		t.Fatal("self parent accepted")
	}
	child, e := s.Folder("child", &f.Object)
	if e != nil {
		t.Fatal(e)
	}
	c := find(t, s, child)
	if _, e = s.Edit(folder, vs.Edit{Name: f.Name, Parent: &c.Object, Tags: []string{}}); e == nil {
		t.Fatal("cycle accepted")
	}
	trashed, e := s.Trash(rev)
	if e != nil {
		t.Fatal(e)
	}
	if !find(t, s, trashed).Trashed {
		t.Fatal("not trashed")
	}
	restored, e := s.Restore(trashed)
	if e != nil {
		t.Fatal(e)
	}
	item := find(t, s, restored.Revision)
	if item.Trashed || item.Parent == nil || *item.Parent != f.Object {
		t.Fatal("parent not restored")
	}
	folderTrash, e := s.Trash(folder)
	if e != nil {
		t.Fatal(e)
	}
	if !find(t, s, restored.Revision).Trashed {
		t.Fatal("descendant visible")
	}
	r, e := s.Restore(restored.Revision)
	if e != nil || !r.AtRoot {
		t.Fatal("missing fallback", e)
	}
	if find(t, s, r.Revision).Parent != nil {
		t.Fatal("fallback not root")
	}
	if _, e = s.Restore(folderTrash); e != nil {
		t.Fatal(e)
	}
}
func TestMultipleTrashRootsAndStaleEdit(t *testing.T) {
	s, _ := testService(t)
	rev := put(t, s, "private-document.txt", nil)
	trash, e := s.Trash(rev)
	if e != nil {
		t.Fatal(e)
	}
	e = s.with(func(x *session) error {
		id, _ := vc.RandomID()
		_, e := x.store.Put(x.master, id.String(), "", vc.Metadata{Type: "folder", Name: "Trash", Tags: []string{TrashTag}}, nil)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	l, _ := s.List()
	roots := 0
	for _, i := range l.Items {
		if i.TrashRoot {
			roots++
		}
	}
	if roots != 2 || !find(t, s, trash).Trashed {
		t.Fatal("trash roots not recognized")
	}
	if _, e = s.Restore(trash); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Edit(rev, vs.Edit{Name: "stale", Tags: []string{}}); e != vs.ErrConflict {
		t.Fatal("stale edit", e)
	}
}
func TestLockCancelsWorkAndRejectsCalls(t *testing.T) {
	s, _ := testService(t)
	rev := put(t, s, "private-document.txt", nil)
	p, e := s.Preview(rev)
	if e != nil || string(p.Data) != "private-body" {
		t.Fatal("preview", e)
	}
	vc.Wipe(p.Data)
	started := make(chan struct{})
	finished := make(chan struct{})
	_, e = s.operation(func(x *session, ctx context.Context, id string) error {
		close(started)
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	})
	if e != nil {
		t.Fatal(e)
	}
	<-started
	s.Lock()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("operation did not cancel")
	}
	if s.State().Unlocked {
		t.Fatal("still unlocked")
	}
	if _, e = s.List(); e != ErrLocked {
		t.Fatal("list after lock", e)
	}
	if _, e = s.Preview(rev); e != ErrLocked {
		t.Fatal("preview after lock", e)
	}
	if e = s.Unlock("wrong", false); e == nil {
		t.Fatal("wrong password accepted")
	}
	if e = s.Unlock("synthetic desktop password", false); e != nil {
		t.Fatal(e)
	}
	if find(t, s, rev).Name != "private-document.txt" {
		t.Fatal("lost document")
	}
}
func TestRecoveryAndNoPlaintextPersistence(t *testing.T) {
	s, code := testService(t)
	rev := put(t, s, "private-document.txt", nil)
	s.Lock()
	b, e := os.ReadFile(code)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Unlock(strings.TrimSpace(string(b)), true); e != nil {
		t.Fatal(e)
	}
	if !s.State().RecoveryOnly {
		t.Fatal("recovery state")
	}
	if _, e = s.Preview(rev); e != nil {
		t.Fatal(e)
	}
	if e = s.Connect("https://example.invalid", false); e == nil {
		t.Fatal("recovery connected")
	}
	s.Lock()
	e = filepath.Walk(filepath.Dir(s.configPath), func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		for _, needle := range []string{"private-document.txt", "private-desktop-tag", "private-body", "synthetic desktop password"} {
			if bytes.Contains(data, []byte(needle)) {
				t.Fatal("plaintext persisted")
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestPreviewLimitsAndExportNoOverwrite(t *testing.T) {
	s, _ := testService(t)
	rev := put(t, s, "private-document.txt", nil)
	path := filepath.Join(t.TempDir(), "existing.txt")
	if e := os.WriteFile(path, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan Event, 10)
	s.emit = func(e Event) {
		if e.Kind == "finished" {
			done <- e
		}
	}
	if _, e := s.Export(rev, path); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e.Error != "EXISTS" {
			t.Fatal(e.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("export stalled")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "keep" {
		t.Fatal("overwritten")
	}
	unsupported := put(t, s, "active.html", nil)
	if _, e := s.Preview(unsupported); e == nil || e.Error() != "PREVIEW_UNSUPPORTED" {
		t.Fatal("active content previewed")
	}
}
