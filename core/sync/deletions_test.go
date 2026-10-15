package sync_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

func trashFixture(t *testing.T, s *vs.Store, key vc.Key, revision string, at int64) string {
	t.Helper()
	root, _ := vc.RandomID()
	if _, e := s.Put(key, root.String(), "", vc.Metadata{Type: "folder", Name: "Trash", Tags: []string{"klefki:system:trash"}}, nil); e != nil {
		t.Fatal(e)
	}
	r, e := s.Revision(key, revision)
	if e != nil {
		t.Fatal(e)
	}
	id := root.String()
	m := r.Metadata
	out, e := s.Edit(key, revision, vs.Edit{Name: m.Name, Parent: &id, Tags: m.Tags, Favorite: m.Favorite, Trash: &vs.TrashState{At: at, PreviousParent: m.Parent}})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func assertGone(t *testing.T, s *vs.Store, key vc.Key, object, revision string) {
	t.Helper()
	heads, e := s.Heads(key)
	if e != nil {
		t.Fatal(e)
	}
	for _, h := range heads {
		if h.Object == object {
			t.Fatal("deleted object returned")
		}
	}
	if e = s.Export(key, revision, &bytes.Buffer{}); e == nil {
		t.Fatal("deleted history can be exported")
	}
	if _, e = s.Check(key); e != nil {
		t.Fatalf("store integrity: %v", e)
	}
}
func TestPermanentDeletionAcrossDevicesAndStaleHistory(t *testing.T) {
	for _, mode := range []string{syncwire.Persistent, syncwire.TemporaryRelay} {
		t.Run(mode, func(t *testing.T) {
			h := setup(t)
			a, ca := h.device(t, "a", true)
			b, cb := h.device(t, "b", false)
			if mode == syncwire.TemporaryRelay {
				if e := ca.SetMode(context.Background(), mode); e != nil {
					t.Fatal(e)
				}
			}
			object, _ := vc.RandomID()
			revision := add(t, a, h.master, object.String(), "", "erase.txt", "permanently deleted text")
			trashed := trashFixture(t, a, h.master, revision, time.Now().UnixMilli())
			index, _ := vc.RandomID()
			data, _ := json.Marshal(map[string]any{"documents": []any{map[string]any{"object": object.String(), "terms": map[string]int{"deleted": 1}}}})
			if _, e := a.Put(h.master, index.String(), "", vc.Metadata{Type: "file", Name: "search-index.v1.json", Tags: []string{"klefki:system:search-index"}, Size: uint64(len(data))}, bytes.NewReader(data)); e != nil {
				t.Fatal(e)
			}
			syncNow(t, ca)
			syncNow(t, cb)
			syncNow(t, ca)
			oldPath := filepath.Join(h.root, "old-backup.db")
			if e := b.Snapshot(oldPath); e != nil {
				t.Fatal(e)
			}
			old, e := b.Revision(h.master, trashed)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = b.Edit(h.master, trashed, vs.Edit{Name: old.Metadata.Name, Tags: old.Metadata.Tags, Trash: &vs.TrashState{}}); e != nil {
				t.Fatal(e)
			}
			p, e := a.PreviewDeletion(h.master, trashed)
			if e != nil || p.Count != 1 {
				t.Fatalf("preview: %+v %v", p, e)
			}
			if e = a.DeleteTrash(h.master, p); e != nil {
				t.Fatal(e)
			}
			assertGone(t, a, h.master, object.String(), revision)
			syncNow(t, ca)
			syncNow(t, cb)
			assertGone(t, b, h.master, object.String(), revision)
			for _, s := range []*vs.Store{a, b} {
				heads, e := s.Heads(h.master)
				if e != nil {
					t.Fatal(e)
				}
				for _, head := range heads {
					if head.Object == index.String() {
						t.Fatal("derived search data retained")
					}
				}
			}
			// An unupdated client must not replay old payloads after deletion is enabled.
			req, _ := http.NewRequest("GET", h.web.URL+"/v1/sync/changes?after=0", nil)
			req.Header.Set("Authorization", "Bearer "+ca.Session.Token)
			res, e := h.web.Client().Do(req)
			if e != nil {
				t.Fatal(e)
			}
			res.Body.Close()
			if res.StatusCode != 428 {
				t.Fatalf("old client status=%d", res.StatusCode)
			}
			// Restore the old device snapshot; its cursor and history cannot undo deletion.
			if e = b.Close(); e != nil {
				t.Fatal(e)
			}
			restored, e := vs.OpenExisting(oldPath, h.account.ID)
			if e != nil {
				t.Fatal(e)
			}
			defer restored.Close()
			cb.Store = restored
			syncNow(t, cb)
			assertGone(t, restored, h.master, object.String(), revision)
			db, e := sql.Open("sqlite", filepath.Join(h.root, "server", "klefki.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			var n int
			if e = db.QueryRow(`SELECT COUNT(*) FROM changes WHERE account=? AND object=? AND (length(metadata)>0 OR file_key IS NOT NULL)`, h.account.ID.String(), object.String()).Scan(&n); e != nil || n != 0 {
				t.Fatalf("server payloads=%d %v", n, e)
			}
			r, e := a.Revision(h.master, trashed)
			if e == nil {
				t.Fatalf("deleted revision retained: %+v", r)
			}
			if e = db.QueryRow(`SELECT COUNT(*) FROM deletion_gc`).Scan(&n); e != nil || n != 0 {
				t.Fatalf("GC pending=%d %v", n, e)
			}
		})
	}
}
func TestTrashRetentionMigrationAndRestore(t *testing.T) {
	h := setup(t)
	s, c := h.device(t, "retention", true)
	object, _ := vc.RandomID()
	revision := add(t, s, h.master, object.String(), "", "legacy.txt", "legacy bytes")
	legacy := trashFixture(t, s, h.master, revision, 0)
	now := time.Now()
	if e := s.InitializeTrash(h.master, now); e != nil {
		t.Fatal(e)
	}
	heads, e := s.Heads(h.master)
	if e != nil {
		t.Fatal(e)
	}
	var dated vs.Head
	for _, head := range heads {
		if head.Object == object.String() {
			dated = head
		}
	}
	if dated.Metadata.TrashedAt != now.UnixMilli() {
		t.Fatal("legacy countdown not initialized")
	}
	if e = s.InitializeTrash(h.master, now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	expired, e := s.ExpiredTrash(h.master, now.Add(vs.TrashRetention-time.Millisecond))
	if e != nil || len(expired) != 0 {
		t.Fatalf("early expiry %v %v", expired, e)
	}
	expired, e = s.ExpiredTrash(h.master, now.Add(vs.TrashRetention))
	if e != nil || len(expired) != 1 {
		t.Fatalf("boundary expiry %v %v", expired, e)
	}
	if _, e = s.Edit(h.master, dated.Revision, vs.Edit{Name: dated.Metadata.Name, Tags: dated.Metadata.Tags, Trash: &vs.TrashState{}}); e != nil {
		t.Fatal(e)
	}
	expired, e = s.ExpiredTrash(h.master, now.Add(2*vs.TrashRetention))
	if e != nil || len(expired) != 0 {
		t.Fatal("restored item expired")
	}
	syncNow(t, c)
	if _, e = s.Revision(h.master, legacy); e != nil {
		t.Fatal("normal trash history removed")
	}
}
func TestAutomaticDeletionRequiresSuccessfulSync(t *testing.T) {
	h := setup(t)
	s, c := h.device(t, "expiry", true)
	object, _ := vc.RandomID()
	revision := add(t, s, h.master, object.String(), "", "expired.txt", "expired bytes")
	trashed := trashFixture(t, s, h.master, revision, time.Now().Add(-vs.TrashRetention-time.Hour).UnixMilli())
	if e := s.BindRemote(h.web.URL); e != nil {
		t.Fatal(e)
	}
	if e := s.CleanupLocalTrash(h.master, time.Now()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Revision(h.master, trashed); e != nil {
		t.Fatal("connected vault cleaned without sync")
	}
	syncNow(t, c)
	assertGone(t, s, h.master, object.String(), revision)
}
func TestFolderDeletionKeepsMovedChildrenAndDeletesLateChildren(t *testing.T) {
	h := setup(t)
	s, c := h.device(t, "folders", true)
	peer, peerClient := h.device(t, "folder-peer", false)
	folder, _ := vc.RandomID()
	parent := folder.String()
	folderRev, e := s.Put(h.master, parent, "", vc.Metadata{Type: "folder", Name: "Folder", Tags: []string{}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	child, _ := vc.RandomID()
	childRev, e := s.Put(h.master, child.String(), "", vc.Metadata{Type: "file", Name: "moved.txt", Parent: &parent, Tags: []string{}, Size: 4}, bytes.NewBufferString("keep"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Edit(h.master, childRev, vs.Edit{Name: "moved.txt", Tags: []string{}}); e != nil {
		t.Fatal(e)
	}
	trashed := trashFixture(t, s, h.master, folderRev, time.Now().UnixMilli())
	syncNow(t, c)
	syncNow(t, peerClient)
	late, _ := vc.RandomID()
	lateRev, e := peer.Put(h.master, late.String(), "", vc.Metadata{Type: "file", Name: "late.txt", Parent: &parent, Tags: []string{}, Size: 4}, bytes.NewBufferString("late"))
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.PreviewDeletion(h.master, trashed)
	if e != nil || p.Count != 1 {
		t.Fatalf("moved child selected: %+v %v", p, e)
	}
	if e = s.DeleteTrash(h.master, p); e != nil {
		t.Fatal(e)
	}
	if e = s.PruneDeleted(h.master); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Revision(h.master, childRev); e != nil {
		t.Fatal("moved child's history removed")
	}
	blocked, _ := vc.RandomID()
	if _, e = s.Put(h.master, blocked.String(), "", vc.Metadata{Type: "file", Name: "late.txt", Parent: &parent, Tags: []string{}, Size: 4}, bytes.NewBufferString("late")); e != vs.ErrConflict {
		t.Fatalf("write into deleted folder: %v", e)
	}
	syncNow(t, c)
	syncNow(t, peerClient)
	syncNow(t, c)
	assertGone(t, peer, h.master, late.String(), lateRev)
	if deleted, e := s.Deleted(late.String()); e != nil || !deleted {
		t.Fatal("late descendant fence not relayed")
	}
}
func TestDeletionSnapshotAndStaleConfirmation(t *testing.T) {
	h := setup(t)
	s, _ := h.device(t, "snapshot", true)
	object, _ := vc.RandomID()
	revision := add(t, s, h.master, object.String(), "", "snapshot.txt", "snapshot bytes")
	trashed := trashFixture(t, s, h.master, revision, time.Now().UnixMilli())
	p, e := s.PreviewDeletion(h.master, trashed)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Revision(h.master, trashed)
	if e != nil {
		t.Fatal(e)
	}
	next, e := s.Edit(h.master, trashed, vs.Edit{Name: "changed.txt", Parent: r.Metadata.Parent, Tags: r.Metadata.Tags})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteTrash(h.master, p); e != vs.ErrConflict {
		t.Fatalf("stale confirmation: %v", e)
	}
	p, e = s.PreviewDeletion(h.master, next)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteTrash(h.master, p); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(h.root, "snapshot.db")
	if e = s.Snapshot(path); e != nil {
		t.Fatal(e)
	}
	restored, e := vs.OpenExisting(path, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if deleted, e := restored.Deleted(object.String()); e != nil || !deleted {
		t.Fatal("backup lost deletion fence")
	}
	if _, e = restored.Check(h.master); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var chunks int
	if e = db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&chunks); e != nil || chunks != 0 {
		t.Fatalf("deleted chunks=%d %v", chunks, e)
	}
	info, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Log(fmt.Sprintf("compacted encrypted snapshot: %d bytes", info.Size()))
}

func TestAutomaticDeletionRejectsAStaleHeadSet(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "cas-a", true)
	if err := ca.SetTrashDefault(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	b, cb := h.device(t, "cas-b", false)
	object, _ := vc.RandomID()
	revision := add(t, a, h.master, object.String(), "", "restore.txt", "restore me")
	trashed := trashFixture(t, a, h.master, revision, time.Now().UnixMilli())
	syncNow(t, ca)
	syncNow(t, cb)
	p, e := a.PreviewDeletion(h.master, trashed)
	if e != nil {
		t.Fatal(e)
	}
	m, e := b.Revision(h.master, trashed)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.Edit(h.master, trashed, vs.Edit{Name: m.Metadata.Name, Tags: m.Metadata.Tags, Trash: &vs.TrashState{}}); e != nil {
		t.Fatal(e)
	}
	syncNow(t, cb)
	body, _ := json.Marshal(syncwire.DeletionBatch{Deletions: p.Targets})
	req, _ := http.NewRequest("POST", h.web.URL+"/v1/sync/deletions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+ca.Session.Token)
	req.Header.Set("X-Klefki-Deletion", "1")
	req.Header.Set("X-Klefki-Content", "1")
	res, e := h.web.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatalf("stale automatic request: %d", res.StatusCode)
	}
	syncNow(t, ca)
	if deleted, e := a.Deleted(object.String()); e != nil || deleted {
		t.Fatal("restored object deleted")
	}
}

func TestLargeExpiredFolderUsesBoundedDeletionBatches(t *testing.T) {
	h := setup(t)
	s, c := h.device(t, "large-folder", true)
	folder, _ := vc.RandomID()
	parent := folder.String()
	revision, e := s.Put(h.master, parent, "", vc.Metadata{Type: "folder", Name: "Large folder", Tags: []string{}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 260; i++ {
		object, _ := vc.RandomID()
		if _, e = s.Put(h.master, object.String(), "", vc.Metadata{Type: "folder", Name: fmt.Sprint(i), Parent: &parent, Tags: []string{}}, nil); e != nil {
			t.Fatal(e)
		}
	}
	trashFixture(t, s, h.master, revision, time.Now().Add(-vs.TrashRetention-time.Hour).UnixMilli())
	syncNow(t, c)
	heads, e := s.Heads(h.master)
	if e != nil {
		t.Fatal(e)
	}
	if len(heads) != 1 || !bytes.Equal([]byte(heads[0].Metadata.Name), []byte("Trash")) {
		t.Fatalf("expired folder contents retained: %d", len(heads))
	}
	if _, e = s.Check(h.master); e != nil {
		t.Fatal(e)
	}
}

func TestServerDeletionCleanupResumesAfterRestart(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failure requires an unprivileged process")
	}
	h := setup(t)
	s, c := h.device(t, "gc-retry", true)
	if err := c.SetTrashDefault(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	object, _ := vc.RandomID()
	revision := add(t, s, h.master, object.String(), "", "gc.txt", "GC retry bytes")
	trashed := trashFixture(t, s, h.master, revision, time.Now().UnixMilli())
	syncNow(t, c)
	accountDir := filepath.Join(h.root, "server", "blobs", h.account.ID.String())
	if e := os.Chmod(accountDir, 0500); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(accountDir, 0700)
	p, e := s.PreviewDeletion(h.master, trashed)
	if e != nil {
		t.Fatal(e)
	}
	for i := range p.Targets {
		p.Targets[i].Heads = nil
	}
	data, _ := json.Marshal(syncwire.DeletionBatch{Deletions: p.Targets})
	req, _ := http.NewRequest("POST", h.web.URL+"/v1/sync/deletions", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.Session.Token)
	req.Header.Set("X-Klefki-Deletion", "1")
	req.Header.Set("X-Klefki-Content", "1")
	res, e := h.web.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 500 {
		t.Fatalf("expected cleanup failure: %d", res.StatusCode)
	}
	if e = os.Chmod(accountDir, 0700); e != nil {
		t.Fatal(e)
	}
	h.web.Close()
	if e = h.backend.Close(); e != nil {
		t.Fatal(e)
	}
	h.start(t)
	syncNow(t, c)
	assertGone(t, s, h.master, object.String(), revision)
	db, e := sql.Open("sqlite", filepath.Join(h.root, "server", "klefki.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var pending int
	if e = db.QueryRow(`SELECT COUNT(*) FROM deletion_gc`).Scan(&pending); e != nil || pending != 0 {
		t.Fatalf("cleanup queue after restart=%d %v", pending, e)
	}
}

func TestDeletionPreservesChunksReferencedByAnotherObject(t *testing.T) {
	h := setup(t)
	s, c := h.device(t, "shared-chunks", true)
	object, _ := vc.RandomID()
	revision := add(t, s, h.master, object.String(), "", "original.txt", "shared encrypted bytes")
	syncNow(t, c)
	original, e := s.Revision(h.master, revision)
	if e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", filepath.Join(h.root, "shared-chunks", "local.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var wrapped []byte
	if e = db.QueryRow(`SELECT file_key FROM revisions WHERE id=?`, revision).Scan(&wrapped); e != nil {
		t.Fatal(e)
	}
	shared, _ := vc.RandomID()
	sharedRevision, _ := vc.RandomID()
	m := original.Metadata
	m.Name = "shared.txt"
	encrypted, e := vc.EncryptMetadata(h.master, h.account.ID, shared, m)
	if e != nil {
		t.Fatal(e)
	}
	wire := syncwire.Revision{ID: sharedRevision.String(), Object: shared.String(), Metadata: encrypted, FileKey: wrapped}
	data, _ := json.Marshal(wire)
	req, _ := http.NewRequest("PUT", h.web.URL+"/v1/sync/revisions/"+wire.ID, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.Session.Token)
	req.Header.Set("X-Klefki-Deletion", "1")
	req.Header.Set("X-Klefki-Content", "1")
	res, e := h.web.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("shared revision publication=%d", res.StatusCode)
	}
	syncNow(t, c)
	trashed := trashFixture(t, s, h.master, revision, time.Now().UnixMilli())
	p, e := s.PreviewDeletion(h.master, trashed)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteTrash(h.master, p); e != nil {
		t.Fatal(e)
	}
	syncNow(t, c)
	var out bytes.Buffer
	if e = s.Export(h.master, wire.ID, &out); e != nil || out.String() != "shared encrypted bytes" {
		t.Fatalf("shared content removed: %v", e)
	}
	chunkPath := filepath.Join(h.root, "server", "blobs", h.account.ID.String(), original.Metadata.FileID, "0")
	if _, e = os.Stat(chunkPath); e != nil {
		t.Fatalf("shared server chunk removed: %v", e)
	}
	trashed = trashFixture(t, s, h.master, wire.ID, time.Now().UnixMilli())
	p, e = s.PreviewDeletion(h.master, trashed)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteTrash(h.master, p); e != nil {
		t.Fatal(e)
	}
	syncNow(t, c)
	if _, e = os.Stat(chunkPath); !os.IsNotExist(e) {
		t.Fatalf("last reference did not release chunk: %v", e)
	}
}
