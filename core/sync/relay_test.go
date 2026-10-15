package sync_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

func relayFiles(t *testing.T, h *harness) int {
	t.Helper()
	n := 0
	e := filepath.WalkDir(filepath.Join(h.root, "server", "blobs"), func(_ string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return n
}
func checkRelayExport(t *testing.T, s *vs.Store, key vc.Key, rev, body string) {
	t.Helper()
	var out bytes.Buffer
	if e := s.Export(key, rev, &out); e != nil || out.String() != body {
		t.Fatalf("local copy: %q, %v", out.String(), e)
	}
}
func TestRelayDeliveryLateJoinAndPersistentSwitch(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "relay-a", true)
	b, cb := h.device(t, "relay-b", false)
	ctx := context.Background()
	obj, _ := vc.RandomID()
	rev := add(t, a, h.master, obj.String(), "", "relay-secret.txt", "relay-only bytes")
	if e := ca.SetMode(ctx, syncwire.TemporaryRelay); e != nil {
		t.Fatal(e)
	}
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if relayFiles(t, h) != 1 {
		t.Fatal("single device must retain relay payload")
	}
	if e := cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	checkRelayExport(t, b, h.master, rev, "relay-only bytes")
	if relayFiles(t, h) != 0 {
		t.Fatal("delivered ciphertext not removed")
	}
	db, e := sql.Open("sqlite", filepath.Join(h.root, "server", "klefki.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var payload int
	if db.QueryRow(`SELECT length(metadata)+COALESCE(length(file_key),0) FROM changes WHERE revision=?`, rev).Scan(&payload) != nil || payload != 0 {
		t.Fatal("encrypted metadata or wrapped key retained after delivery")
	}
	// A login creates a new session, but retains the local device identity and receipt.
	before, _ := b.RelayState()
	cb.Session, _ = cb.Remote.Login(ctx, h.account.ID, h.credential)
	if e := cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	after, _ := b.RelayState()
	if before.Device != after.Device || len(after.Known) != 2 {
		t.Fatal("session counted as a new device")
	}
	if relayFiles(t, h) != 0 {
		t.Fatal("idle sync unnecessarily reuploaded delivered data")
	}
	// A new device must wait, then an existing device can reseed it asynchronously.
	c, cc := h.device(t, "relay-c", false)
	if e := cc.Sync(ctx); !errors.Is(e, vs.ErrRelayWaiting) {
		t.Fatalf("late join: %v", e)
	}
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if relayFiles(t, h) != 1 {
		t.Fatal("late join was not reseeded")
	}
	if e := cc.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	checkRelayExport(t, c, h.master, rev, "relay-only bytes")
	if relayFiles(t, h) != 0 {
		t.Fatal("late delivery not removed")
	}
	// Metadata edits reuse content. An offline linked device must retain that content.
	edit, e := a.Edit(h.master, rev, vs.Edit{Name: "renamed.txt", Tags: []string{}, Favorite: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e = cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if relayFiles(t, h) != 1 {
		t.Fatal("offline recipient lost shared chunks")
	}
	if e = cc.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	checkRelayExport(t, c, h.master, edit, "relay-only bytes")
	if relayFiles(t, h) != 0 {
		t.Fatal("metadata delivery retained chunks")
	}
	if e = ca.SetMode(ctx, syncwire.Persistent); e != nil {
		t.Fatal(e)
	}
	if e = ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if relayFiles(t, h) != 1 {
		t.Fatal("persistent mode did not restore document")
	}
	d, cd := h.device(t, "persistent-d", false)
	if e = cd.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	checkRelayExport(t, d, h.master, edit, "relay-only bytes")
}

func TestRelayQuotaRetainsPendingAndCanDrain(t *testing.T) {
	h := setup(t)
	h.relayLimit = 1200
	h.web.Close()
	h.backend.Close()
	h.start(t)
	a, ca := h.device(t, "quota-a", true)
	b, cb := h.device(t, "quota-b", false)
	c, cc := h.device(t, "quota-offline", false)
	ctx := context.Background()
	if e := ca.SetMode(ctx, syncwire.TemporaryRelay); e != nil {
		t.Fatal(e)
	}
	if e := cc.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	obj, _ := vc.RandomID()
	first := add(t, a, h.master, obj.String(), "", "one.txt", strings.Repeat("a", 300))
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	obj, _ = vc.RandomID()
	second := add(t, a, h.master, obj.String(), "", "two.txt", strings.Repeat("b", 300))
	e := ca.Sync(ctx)
	var he *vs.HTTPError
	if !errors.As(e, &he) || he.Status != 507 {
		t.Fatalf("quota: %v", e)
	}
	if pending, _ := a.Pending(); pending != 1 {
		t.Fatal("quota lost pending edit")
	}
	checkRelayExport(t, a, h.master, second, strings.Repeat("b", 300))
	if e = cb.Sync(ctx); e != nil && !errors.Is(e, vs.ErrContentUnavailable) {
		t.Fatal(e)
	}
	checkRelayExport(t, b, h.master, first, strings.Repeat("a", 300))
	offline, e := c.RelayState()
	if e != nil {
		t.Fatal(e)
	}
	if e = ca.ForgetDevice(ctx, offline.Device); e != nil {
		t.Fatalf("cannot release offline peer while queue is full: %v", e)
	}
	if e = ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e = cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	checkRelayExport(t, b, h.master, second, strings.Repeat("b", 300))
	if relayFiles(t, h) != 0 {
		t.Fatal("quota recovery left delivered content")
	}
}

func TestRelayForgottenDevicesAndRetiredRevisionIntegrity(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "forget-a", true)
	_, cb := h.device(t, "forget-b", false)
	_, cc := h.device(t, "forget-c", false)
	ctx := context.Background()
	if e := ca.SetMode(ctx, syncwire.TemporaryRelay); e != nil {
		t.Fatal(e)
	}
	if e := cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e := cc.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	obj, _ := vc.RandomID()
	rev := add(t, a, h.master, obj.String(), "", "forgotten.txt", "receipt test")
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e := cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	state, _ := cc.Store.RelayState()
	if relayFiles(t, h) != 1 {
		t.Fatal("offline device not holding delivery")
	}
	if e := ca.ForgetDevice(ctx, state.Device); e != nil {
		t.Fatal(e)
	}
	if relayFiles(t, h) != 0 {
		t.Fatal("forgotten recipient still holds payload")
	}
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e := cc.Sync(ctx); e == nil {
		t.Fatal("forgotten device rejoined with old identity")
	}
	// A retired ID still rejects changed ciphertext, even though its payload was removed.
	var record syncwire.Revision
	db, e := sql.Open("sqlite", filepath.Join(h.root, "forget-a", "local.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = db.QueryRow(`SELECT id,object,base,metadata,file_key FROM revisions WHERE id=?`, rev).Scan(&record.ID, &record.Object, &record.Base, &record.Metadata, &record.FileKey); e != nil {
		t.Fatal(e)
	}
	record.Metadata[len(record.Metadata)-1] ^= 1
	body, _ := json.Marshal(record)
	req, _ := http.NewRequest("PUT", h.web.URL+"/v1/sync/revisions/"+rev, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+ca.Session.Token)
	state, _ = a.RelayState()
	req.Header.Set("X-Klefki-Device", state.Device)
	req.Header.Set("X-Klefki-Content", "1")
	res, e := h.web.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatalf("retired immutable ID accepted: %d", res.StatusCode)
	}
}

func TestRelayRecoversFromCompleteServerLoss(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "reset-a", true)
	b, cb := h.device(t, "reset-b", false)
	ctx := context.Background()
	obj, _ := vc.RandomID()
	rev := add(t, a, h.master, obj.String(), "", "before.txt", "before reset")
	if e := ca.SetMode(ctx, syncwire.TemporaryRelay); e != nil {
		t.Fatal(e)
	}
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e := cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	state, _ := a.RelayState()
	obj, _ = vc.RandomID()
	queued := add(t, a, h.master, obj.String(), "", "queued.txt", "uploaded but not delivered")
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if n, _ := a.Pending(); n != 0 {
		t.Fatal("fixture upload not accepted")
	}
	h.web.Close()
	h.backend.Close()
	if e := os.RemoveAll(filepath.Join(h.root, "server")); e != nil {
		t.Fatal(e)
	}
	h.start(t)
	// Restore using the same server URL and expired in-memory token.
	remote, e := vs.NewRemote(h.web.URL, h.web.Client(), false)
	if e != nil {
		t.Fatal(e)
	}
	ca.Remote = remote
	if e = ca.SyncAccount(ctx, h.account, h.credential); e != nil {
		t.Fatal(e)
	}
	restored, _ := a.RelayState()
	if restored.Epoch == state.Epoch || restored.Mode != syncwire.TemporaryRelay || len(restored.Known) != 2 {
		t.Fatal("server generation or membership not restored")
	}
	if relayFiles(t, h) != 2 {
		t.Fatal("offline device's undelivered data was discarded")
	}
	cb.Remote, _ = vs.NewRemote(h.web.URL, h.web.Client(), false)
	if e = cb.SyncAccount(ctx, h.account, h.credential); e != nil {
		t.Fatal(e)
	}
	checkRelayExport(t, b, h.master, rev, "before reset")
	checkRelayExport(t, b, h.master, queued, "uploaded but not delivered")
	if relayFiles(t, h) != 0 {
		t.Fatal("recovered deliveries not removed")
	}
}

func TestRelayRequiresUpgradedClient(t *testing.T) {
	h := setup(t)
	_, ca := h.device(t, "upgraded", true)
	ctx := context.Background()
	if e := ca.SetMode(ctx, syncwire.TemporaryRelay); e != nil {
		t.Fatal(e)
	}
	legacy, e := vs.NewRemote(h.web.URL, h.web.Client(), false)
	if e != nil {
		t.Fatal(e)
	}
	session, e := legacy.Login(ctx, h.account.ID, h.credential)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", h.web.URL+"/v1/sync/changes?after=0", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	res, e := h.web.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 428 {
		t.Fatalf("legacy relay request: %d", res.StatusCode)
	}
}

func TestRelayRestoredSnapshotWaitsForNewerHistory(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "snapshot-a", true)
	_, cb := h.device(t, "snapshot-b", false)
	ctx := context.Background()
	if e := ca.SetMode(ctx, syncwire.TemporaryRelay); e != nil {
		t.Fatal(e)
	}
	obj, _ := vc.RandomID()
	add(t, a, h.master, obj.String(), "", "older.txt", "older snapshot bytes")
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e := cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	snapshot := filepath.Join(h.root, "snapshot.db")
	if e := a.Snapshot(snapshot); e != nil {
		t.Fatal(e)
	}
	obj, _ = vc.RandomID()
	newer := add(t, a, h.master, obj.String(), "", "newer.txt", "newer retained bytes")
	if e := ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e := cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if relayFiles(t, h) != 0 {
		t.Fatal("newer history not purged before restore")
	}
	if e := a.Close(); e != nil {
		t.Fatal(e)
	}
	restored, e := vs.Open(snapshot, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	ca.Store = restored
	if e = ca.Sync(ctx); !errors.Is(e, vs.ErrRelayWaiting) {
		t.Fatalf("restored snapshot should wait for newer history: %v", e)
	}
	if e = cb.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	if e = ca.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	checkRelayExport(t, restored, h.master, newer, "newer retained bytes")
	if relayFiles(t, h) != 0 {
		t.Fatal("restored delivery retained payloads")
	}
}
