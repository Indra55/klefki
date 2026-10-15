package sync_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

func TestDeviceStateReportsOfflineWorkAndReceiverIndependentDelivery(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "pending-source", true)
	b, cb := h.device(t, "pending-receiver", false)
	ctx := context.Background()
	if err := ca.SetMode(ctx, syncwire.TemporaryRelay); err != nil {
		t.Fatal(err)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	source, _ := a.RelayState()
	var posts atomic.Int32
	var failReport atomic.Bool
	transport := ca.Remote
	counting := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/sync/device-state" {
			posts.Add(1)
			if failReport.Load() {
				return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("fixture report unavailable"))}, nil
			}
		}
		return h.web.Client().Transport.RoundTrip(r)
	})}
	remote, err := vs.NewRemote(h.web.URL, counting, false)
	if err != nil {
		t.Fatal(err)
	}
	ca.Remote = remote
	// Prepare establishes the stable identity on the replacement transport.
	syncNow(t, ca)
	report := func() *syncwire.DeviceState {
		t.Helper()
		peers, err := ca.Remote.Status(ctx, ca.Session)
		if err != nil {
			t.Fatal(err)
		}
		out, err := ca.ReportDeviceState(ctx, peers)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	clean := report()
	if clean == nil || clean.Pending {
		t.Fatal("initial clean report missing", clean)
	}
	if report() != nil || posts.Load() != 1 {
		t.Fatal("idle reports caused network writes", posts.Load())
	}
	// Save a pre-edit snapshot to reproduce local backup restoration later.
	a.Close()
	path := filepath.Join(h.root, "pending-source", "local.db")
	backup, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err = vs.Open(path, h.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	ca.Store = a
	obj, _ := vc.RandomID()
	revision := add(t, a, h.master, obj.String(), "", "asynchronous.txt", "delivered without overlap")
	failReport.Store(true)
	beforeFailure, _ := ca.Remote.Status(ctx, ca.Session)
	if _, err := ca.ReportDeviceState(ctx, beforeFailure); err == nil {
		t.Fatal("report failure swallowed")
	}
	failReport.Store(false)
	pending := report()
	if pending == nil || !pending.Pending || pending.Version <= clean.Version {
		t.Fatal("local edit not reported", pending)
	}
	peers, err := cb.Remote.Status(ctx, cb.Session)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range peers.Devices {
		if d.Device == source.Device {
			found = d.DeviceState != nil && d.DeviceState.Pending
		}
	}
	if !found {
		t.Fatal("receiver cannot see offline source warning")
	}
	if needed, err := cb.NeedsSync(ctx); err != nil || needed {
		t.Fatal("peer warning started needless sync", needed, err)
	}
	// Receiver does no work during upload; source then closes before receiver syncs.
	syncNow(t, ca)
	uploaded := report()
	if uploaded == nil || uploaded.Pending {
		t.Fatal("confirmed upload did not clear pending report")
	}
	a.Close()
	syncNow(t, cb)
	checkRelayExport(t, b, h.master, revision, "delivered without overlap")
	// Restoring a lower-version snapshot must supersede the server's saved report.
	if err = os.WriteFile(path, backup, 0600); err != nil {
		t.Fatal(err)
	}
	a, err = vs.Open(path, h.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ca.Store = a
	ca.Remote = transport
	restored := report()
	if restored == nil || restored.Pending || restored.Version <= uploaded.Version {
		t.Fatal("restored version did not reconcile", restored)
	}
	// Old servers without capability must not receive the new POST.
	before := posts.Load()
	if out, err := ca.ReportDeviceState(ctx, syncwire.Status{}); err != nil || out != nil || posts.Load() != before {
		t.Fatal("old-server fallback failed", out, err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeviceStateIncludesPermanentDeletions(t *testing.T) {
	h := setup(t)
	s, client := h.device(t, "deletion-source", true)
	object, _ := vc.RandomID()
	revision := add(t, s, h.master, object.String(), "", "delete-me.txt", "deletion report fixture")
	trashed := trashFixture(t, s, h.master, revision, time.Now().UnixMilli())
	syncNow(t, client)
	state, err := s.DeviceState(0)
	if err != nil || state.Pending {
		t.Fatal("initial upload not clean", state, err)
	}
	preview, err := s.PreviewDeletion(h.master, trashed)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteTrash(h.master, preview); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Pending(); err != nil || n != 1 {
		t.Fatal("expected only pending deletion", n, err)
	}
	pending, err := s.DeviceState(0)
	if err != nil || !pending.Pending || pending.Version <= state.Version {
		t.Fatal("deletion missing from report", pending, err)
	}
	syncNow(t, client)
	clean, err := s.DeviceState(0)
	if err != nil || clean.Pending || clean.Version <= pending.Version {
		t.Fatal("acknowledged deletion not cleared", clean, err)
	}
}

func TestDeviceStateRepublishesAfterCompleteServerLoss(t *testing.T) {
	h := setup(t)
	s, client := h.device(t, "recovering-source", true)
	ctx := context.Background()
	if err := client.SetMode(ctx, syncwire.TemporaryRelay); err != nil {
		t.Fatal(err)
	}
	syncNow(t, client)
	object, _ := vc.RandomID()
	add(t, s, h.master, object.String(), "", "recovering.txt", "source still retains its bytes")
	status, err := client.Remote.Status(ctx, client.Session)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := client.ReportDeviceState(ctx, status)
	if err != nil || pending == nil || !pending.Pending {
		t.Fatal(pending, err)
	}
	oldEpoch := status.Epoch
	h.web.Close()
	h.backend.Close()
	if err = os.RemoveAll(filepath.Join(h.root, "server")); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	client.Remote, err = vs.NewRemote(h.web.URL, h.web.Client(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SyncAccount(ctx, h.account, h.credential); err != nil {
		t.Fatal(err)
	}
	status, err = client.Remote.Status(ctx, client.Session)
	if err != nil || status.Epoch == oldEpoch {
		t.Fatal("server epoch not replaced", status, err)
	}
	for _, d := range status.Devices {
		if d.DeviceState != nil {
			t.Fatal("lost server unexpectedly retained report")
		}
	}
	clean, err := client.ReportDeviceState(ctx, status)
	if err != nil || clean == nil || clean.Pending || clean.Version <= pending.Version {
		t.Fatal("recovered report missing", clean, err)
	}
}
