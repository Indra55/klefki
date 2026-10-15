package sync_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

func TestSelectiveOffTransfersNoTrashBlobsAndPersists(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "no-trash-a", true)
	b, cb := h.device(t, "no-trash-b", false)
	object, _ := vc.RandomID()
	revision := add(t, a, h.master, object.String(), "", "omitted.txt", "private synthetic bytes")
	for i := 0; i < 25; i++ {
		id, _ := vc.RandomID()
		if _, err := a.Put(h.master, id.String(), "", vc.Metadata{Type: "folder", Name: "page fixture", Tags: []string{}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	trashed := trashFixture(t, a, h.master, revision, time.Now().UnixMilli())
	calls := 0
	for _, client := range []*vs.Client{ca, cb} {
		client.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Path, "/blobs/") {
				calls++
			}
			return h.web.Client().Transport.RoundTrip(r)
		})}, false)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	if calls != 0 {
		t.Fatal("Trash caused blob traffic", calls)
	}
	if err := cb.EnsureContent(context.Background(), trashed); !errors.Is(err, vs.ErrContentUnavailable) {
		t.Fatal("missing source was not reported", err)
	}
	if err := b.SetTrashOverride("off"); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := b.Snapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	copy, err := vs.OpenExisting(snapshot, h.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	p, err := copy.TrashPolicy()
	if err != nil || p.Override != "off" || p.Effective {
		t.Fatal("policy lost", p, err)
	}
	checked, err := copy.Check(h.master)
	if err != nil {
		t.Fatal("deferred content treated as corrupt", err)
	}
	missing := 0
	for _, r := range checked {
		if r.ContentMissing {
			missing++
		}
	}
	if missing != 2 {
		t.Fatal("incomplete backup not identified", missing)
	}
	if err = ca.SetTrashDefault(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	if content(t, b, h.master, trashed) != "private synthetic bytes" {
		t.Fatal("durable explicit recovery request did not resume")
	}
}

func TestSelectiveRelayReseedsEnabledTrashAfterCleanup(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "reseed-a", true)
	_, cb := h.device(t, "reseed-b", false)
	if err := ca.SetMode(context.Background(), syncwire.TemporaryRelay); err != nil {
		t.Fatal(err)
	}
	object, _ := vc.RandomID()
	rev := add(t, a, h.master, object.String(), "", "reseed.txt", "recover after cleanup")
	trash := trashFixture(t, a, h.master, rev, time.Now().UnixMilli())
	syncNow(t, ca)
	syncNow(t, cb)
	syncNow(t, ca)
	late, cc := h.device(t, "reseed-c", false)
	if err := cc.Sync(context.Background()); !errors.Is(err, vs.ErrRelayWaiting) {
		t.Fatal("late metadata wait", err)
	}
	syncNow(t, ca)
	syncNow(t, cc)
	if err := late.SetTrashOverride("on"); err != nil {
		t.Fatal(err)
	}
	if err := cc.Sync(context.Background()); !errors.Is(err, vs.ErrContentUnavailable) {
		t.Fatal("missing contents not requested", err)
	}
	if err := a.SetTrashOverride("on"); err != nil {
		t.Fatal(err)
	}
	if needed, err := ca.NeedsSync(context.Background()); err != nil || !needed {
		t.Fatal("source did not detect content request", needed, err)
	}
	syncNow(t, ca)
	syncNow(t, cc)
	if content(t, late, h.master, trash) != "recover after cleanup" {
		t.Fatal("relay recovery failed")
	}
}

func TestSelectiveCapabilityAndDeletionFence(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "gate-a", true)
	_, cb := h.device(t, "gate-b", false)
	object, _ := vc.RandomID()
	rev := add(t, a, h.master, object.String(), "", "delete.txt", "bytes")
	trash := trashFixture(t, a, h.master, rev, time.Now().UnixMilli())
	syncNow(t, ca)
	syncNow(t, cb)
	request, _ := http.NewRequest("GET", h.web.URL+"/v1/sync/changes?after=0", nil)
	request.Header.Set("Authorization", "Bearer "+ca.Session.Token)
	request.Header.Set("X-Klefki-Deletion", "1")
	response, err := h.web.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 428 {
		t.Fatal("legacy client bypassed capability gate", response.StatusCode)
	}
	preview, err := a.PreviewDeletion(h.master, trash)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.DeleteTrash(h.master, preview); err != nil {
		t.Fatal(err)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	if err = cb.EnsureContent(context.Background(), trash); err == nil {
		t.Fatal("permanently deleted file restored")
	}
}

func TestSelectivePolicyChangeStopsTrashTransfer(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "stop-a", true)
	if err := a.SetTrashOverride("on"); err != nil {
		t.Fatal(err)
	}
	object, _ := vc.RandomID()
	body := bytes.Repeat([]byte("x"), vc.ChunkSize*6)
	rev, err := a.Put(h.master, object.String(), "", vc.Metadata{Type: "file", Name: "large.txt", Tags: []string{}, Size: uint64(len(body))}, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	trashFixture(t, a, h.master, rev, time.Now().UnixMilli())
	changed := false
	ca.Observe = func(p syncwire.Progress) {
		if p.Phase == "uploading" && p.Chunk > 0 && !changed {
			changed = true
			if err := a.SetTrashOverride("off"); err != nil {
				t.Error(err)
			}
		}
	}
	syncNow(t, ca)
	if !changed {
		t.Fatal("fixture did not change policy during transfer")
	}
	if pending, err := a.Pending(); err != nil || pending != 0 {
		t.Fatal("excluded work stuck pending", pending, err)
	}
}

func TestSelectiveUploadWorkSurvivesMetadataInterruption(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "interrupted-a", true)
	b, cb := h.device(t, "interrupted-b", false)
	object, _ := vc.RandomID()
	revision := add(t, a, h.master, object.String(), "", "durable.txt", "durable upload bytes")
	ca.Remote, _ = vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/sync/changes" {
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("fixture unavailable"))}, nil
		}
		return h.web.Client().Transport.RoundTrip(r)
	})}, false)
	ca.Retry.Attempts = 1
	if err := ca.Sync(context.Background()); err == nil {
		t.Fatal("interruption not injected")
	}
	if pending, err := a.Pending(); err != nil || pending != 1 {
		t.Fatal("upload work lost after metadata publication", pending, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := vs.OpenExisting(filepath.Join(h.root, "interrupted-a", "local.db"), h.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	ca.Store = reopened
	ca.Remote, _ = vs.NewRemote(h.web.URL, h.web.Client(), false)
	syncNow(t, ca)
	syncNow(t, cb)
	if content(t, b, h.master, revision) != "durable upload bytes" {
		t.Fatal("restart did not resume upload")
	}
}

func TestSelectiveActiveConflictStillTransfers(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "conflict-a", true)
	b, cb := h.device(t, "conflict-b", false)
	object, _ := vc.RandomID()
	original := add(t, a, h.master, object.String(), "", "conflict.txt", "active conflict bytes")
	syncNow(t, ca)
	syncNow(t, cb)
	trashed := trashFixture(t, a, h.master, original, time.Now().UnixMilli())
	active, err := b.Edit(h.master, original, vs.Edit{Name: "active branch.txt", Tags: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	syncNow(t, ca)
	late, cc := h.device(t, "conflict-c", false)
	syncNow(t, cc)
	if content(t, late, h.master, active) != "active conflict bytes" {
		t.Fatal("active branch excluded with Trash off")
	}
	heads, err := late.Heads(h.master)
	if err != nil {
		t.Fatal(err)
	}
	branches := 0
	for _, head := range heads {
		if head.Object == object.String() {
			if !head.Conflict {
				t.Fatal("conflict hidden")
			}
			if head.Revision != trashed && head.Revision != active {
				t.Fatal("unexpected head")
			}
			branches++
		}
	}
	if branches != 2 {
		t.Fatal("conflict branch lost", branches)
	}
}

func TestSelectiveNewActiveWorkPreemptsRemainingTrash(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "preempt-a", true)
	if err := a.SetTrashOverride("on"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old-a.txt", "old-b.txt"} {
		object, _ := vc.RandomID()
		revision := add(t, a, h.master, object.String(), "", name, "trash bytes")
		trashFixture(t, a, h.master, revision, time.Now().UnixMilli())
	}
	names := []string{}
	inserted := false
	ca.Observe = func(p syncwire.Progress) {
		if p.Phase == "uploading" && (len(names) == 0 || names[len(names)-1] != p.Name) {
			names = append(names, p.Name)
		}
		if p.Phase == "uploading" && p.Chunk > 0 && !inserted {
			inserted = true
			object, _ := vc.RandomID()
			_, err := a.Put(h.master, object.String(), "", vc.Metadata{Type: "file", Name: "new.txt", Tags: []string{}, Size: 3}, strings.NewReader("new"))
			if err != nil {
				t.Error(err)
			}
		}
	}
	syncNow(t, ca)
	if len(names) != 3 || names[0] != "old-a.txt" || names[1] != "new.txt" || names[2] != "old-b.txt" {
		t.Fatal("new active work did not preempt Trash", names)
	}
}

func TestSelectiveTrashPolicyAndRestore(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "selective-a", true)
	b, cb := h.device(t, "selective-b", false)
	root, _ := vc.RandomID()
	if _, err := a.Put(h.master, root.String(), "", vc.Metadata{Type: "folder", Name: "Trash", Tags: []string{"klefki:system:trash"}}, nil); err != nil {
		t.Fatal(err)
	}
	object, _ := vc.RandomID()
	old := add(t, a, h.master, object.String(), "", "old.txt", "trash contents")
	parent := root.String()
	trashed, err := a.Edit(h.master, old, vs.Edit{Name: "old.txt", Parent: &parent, Tags: []string{}, Trash: &vs.TrashState{At: time.Now().UnixMilli()}})
	if err != nil {
		t.Fatal(err)
	}
	live, _ := vc.RandomID()
	active := add(t, a, h.master, live.String(), "", "active.txt", "active contents")
	syncNow(t, ca)
	syncNow(t, cb)
	if content(t, b, h.master, active) != "active contents" {
		t.Fatal("active file missing")
	}
	if _, err = b.Revision(h.master, trashed); err != nil {
		t.Fatal("trash listing missing", err)
	}
	if err = b.Export(h.master, trashed, io.Discard); err == nil {
		t.Fatal("trash downloaded with default off")
	}
	if pending, _ := b.Pending(); pending != 0 {
		t.Fatal("excluded trash reported pending", pending)
	}

	if err = ca.SetTrashDefault(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err = b.SetTrashOverride("off"); err != nil {
		t.Fatal(err)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	if err = b.Export(h.master, trashed, io.Discard); err == nil {
		t.Fatal("device off did not override vault on")
	}
	if err = cb.EnsureContent(context.Background(), trashed); err != nil {
		t.Fatal(err)
	}
	if content(t, b, h.master, trashed) != "trash contents" {
		t.Fatal("explicit restore fetch failed")
	}
	if err = ca.SetTrashDefault(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if err = a.SetTrashOverride("on"); err != nil {
		t.Fatal(err)
	}
	syncNow(t, ca)
	if content(t, b, h.master, trashed) != "trash contents" {
		t.Fatal("changing policy erased cached content")
	}
}

func TestSelectiveActiveBeforeTrashAndNestedFolder(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "priority-a", true)
	b, cb := h.device(t, "priority-b", false)
	if err := ca.SetTrashDefault(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	root, _ := vc.RandomID()
	_, err := a.Put(h.master, root.String(), "", vc.Metadata{Type: "folder", Name: "Trash", Tags: []string{"klefki:system:trash"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	folder, _ := vc.RandomID()
	parent := root.String()
	_, err = a.Put(h.master, folder.String(), "", vc.Metadata{Type: "folder", Name: "Nested", Tags: []string{}, Parent: &parent, TrashedAt: time.Now().UnixMilli()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := vc.RandomID()
	parent = folder.String()
	trash, err := a.Put(h.master, file.String(), "", vc.Metadata{Type: "file", Name: "nested.txt", Tags: []string{}, Parent: &parent, Size: 5}, strings.NewReader("trash"))
	if err != nil {
		t.Fatal(err)
	}
	live, _ := vc.RandomID()
	active := add(t, a, h.master, live.String(), "", "live.txt", "live")
	var uploads, downloads []string
	ca.Observe = func(p syncwire.Progress) {
		if p.Phase == "uploading" && (len(uploads) == 0 || uploads[len(uploads)-1] != p.Revision) {
			uploads = append(uploads, p.Revision)
		}
	}
	cb.Observe = func(p syncwire.Progress) {
		if p.Phase == "downloading" && (len(downloads) == 0 || downloads[len(downloads)-1] != p.Revision) {
			downloads = append(downloads, p.Revision)
		}
	}
	syncNow(t, ca)
	syncNow(t, cb)
	if len(uploads) != 2 || uploads[0] != active || uploads[1] != trash {
		t.Fatal("upload order", uploads)
	}
	if len(downloads) != 2 || downloads[0] != active || downloads[1] != trash {
		t.Fatal("download order", downloads)
	}
	if content(t, b, h.master, trash) != "trash" {
		t.Fatal("nested trash missing")
	}
}
