package desktop

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

func TestFreshDownloadPreservesAccountAndOriginalStore(t *testing.T) {
	s, _ := testService(t)
	revision := put(t, s, "retained.txt", nil)
	x, _, web := syncServer(t, s, nil)
	s.SyncNow()
	awaitSync(t, s)
	if err := s.SetSyncPaused(true); err != nil {
		t.Fatal(err)
	}
	before := s.State()
	account, err := os.ReadFile(before.Config.AccountPath)
	if err != nil {
		t.Fatal(err)
	}
	master, accountID := x.master, x.account.ID
	remote, _ := vs.NewRemote(web.URL, web.Client(), false)
	if err = s.freshDownload(remote); err != nil {
		t.Fatal(err)
	}
	after := s.State()
	if after.Unlocked || after.Config.StorePath == before.Config.StorePath || after.Config.PreviousStorePath != before.Config.StorePath || after.Config.AccountPath != before.Config.AccountPath || after.Config.Server != before.Config.Server || !after.Config.SyncPaused || after.Config.FreshDownload != nil {
		t.Fatal("fresh download changed account access or lost original path")
	}
	actual, err := os.ReadFile(before.Config.AccountPath)
	if err != nil || !bytes.Equal(account, actual) {
		t.Fatal("account file changed", err)
	}
	old, err := vs.OpenExisting(before.Config.StorePath, accountID)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err = old.Check(master); err != nil {
		t.Fatal("original store invalid", err)
	}
	if err = s.Unlock("synthetic desktop password", false); err != nil {
		t.Fatal(err)
	}
	if find(t, s, revision).Name != "retained.txt" {
		t.Fatal("fresh copy lost document")
	}
}

func TestFreshDownloadRefusesPendingLocalWork(t *testing.T) {
	s, _ := testService(t)
	put(t, s, "pending.txt", nil)
	_, _, web := syncServer(t, s, nil)
	before := s.State()
	remote, _ := vs.NewRemote(web.URL, web.Client(), false)
	if err := s.freshDownload(remote); Code(err) != "SYNC_DOWNLOAD_PENDING" {
		t.Fatal(err)
	}
	after := s.State()
	if !after.Unlocked || before.Config.StorePath != after.Config.StorePath || after.Config.FreshDownload != nil {
		t.Fatal("pending work changed selection")
	}
}

func TestFreshDownloadRelayRetryReusesStagingDevice(t *testing.T) {
	s, _ := testService(t)
	put(t, s, "relay.txt", nil)
	_, _, web := syncServer(t, s, nil)
	s.SyncNow()
	awaitSync(t, s)
	if err := s.SetSyncMode(syncwire.TemporaryRelay); err != nil {
		t.Fatal(err)
	}
	awaitSync(t, s)
	if err := s.SetSyncPaused(true); err != nil {
		t.Fatal(err)
	}
	before := s.State()
	// Two recipients allow delivery ciphertext to be collected from the relay.
	other := filepath.Join(t.TempDir(), "other.db")
	s.mu.Lock()
	x := s.session
	s.mu.Unlock()
	peer, err := vs.Open(other, x.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	peerRemote, _ := vs.NewRemote(web.URL, web.Client(), false)
	auth, err := peerRemote.Login(x.ctx, x.account.ID, x.credential)
	if err != nil {
		t.Fatal(err)
	}
	c := vs.Client{Store: peer, Remote: peerRemote, Session: auth, Master: x.master}
	if err = c.Sync(x.ctx); err != nil {
		t.Fatal(err)
	}
	peer.Close()
	var path, device string
	for attempt := 0; attempt < 2; attempt++ {
		remote, _ := vs.NewRemote(web.URL, web.Client(), false)
		err = s.freshDownload(remote)
		if Code(err) != "RELAY_WAITING" {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		after := s.State()
		if !after.Unlocked || after.Config.StorePath != before.Config.StorePath || after.Config.FreshDownload == nil {
			t.Fatal("failed relay download replaced current store")
		}
		stage, err := vs.OpenExisting(after.Config.FreshDownload.Path, x.account.ID)
		if err != nil {
			t.Fatal(err)
		}
		relay, err := stage.RelayState()
		stage.Close()
		if err != nil {
			t.Fatal(err)
		}
		if attempt > 0 && (path != after.Config.FreshDownload.Path || device != relay.Device) {
			t.Fatal("retry accumulated a new store/device")
		}
		path, device = after.Config.FreshDownload.Path, relay.Device
	}
	// The current local device can now re-supply the missing ciphertext.
	s.SetSyncPaused(false)
	awaitSync(t, s)
	s.SetSyncPaused(true)
	remote, _ := vs.NewRemote(web.URL, web.Client(), false)
	if err = s.freshDownload(remote); err != nil {
		t.Fatal("peer bootstrap did not finish", err)
	}
}

func TestFreshDownloadCancellationRetainsSelection(t *testing.T) {
	s, _ := testService(t)
	put(t, s, "cancel.txt", nil)
	var stall atomic.Bool
	started := make(chan struct{}, 1)
	_, _, web := syncServer(t, s, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if stall.Load() && r.URL.Path == "/v1/sync/changes" {
				select {
				case started <- struct{}{}:
				default:
				}
				<-r.Context().Done()
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	s.SyncNow()
	awaitSync(t, s)
	s.SetSyncPaused(true)
	before := s.State()
	stall.Store(true)
	remote, _ := vs.NewRemote(web.URL, web.Client(), false)
	done := make(chan error, 1)
	go func() { done <- s.freshDownload(remote) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}
	locked := make(chan struct{})
	go func() { s.Lock(); close(locked) }()
	select {
	case err := <-done:
		if Code(err) != "LOCKED" {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock did not cancel download")
	}
	<-locked
	if after := s.State(); after.Unlocked || after.Config.StorePath != before.Config.StorePath || after.Config.FreshDownload == nil {
		t.Fatal("cancelled download changed source")
	}
}

func TestFreshDownloadSaveFailureKeepsOriginalSelected(t *testing.T) {
	s, _ := testService(t)
	put(t, s, "rollback.txt", nil)
	_, _, web := syncServer(t, s, nil)
	s.SyncNow()
	awaitSync(t, s)
	s.SetSyncPaused(true)
	before := s.State()
	// Force the first staging-config write to fail without touching the real config.
	s.mu.Lock()
	s.configPath = filepath.Join(t.TempDir(), "absent", "settings.json")
	s.mu.Unlock()
	remote, _ := vs.NewRemote(web.URL, web.Client(), false)
	if err := s.freshDownload(remote); err == nil {
		t.Fatal("save failure accepted")
	}
	after := s.State()
	if !after.Unlocked || after.Config.StorePath != before.Config.StorePath || after.Config.FreshDownload != nil {
		t.Fatal("save failure changed source")
	}
}

func TestFreshDownloadRefusesMissingKnownHistory(t *testing.T) {
	s, _ := testService(t)
	put(t, s, "known.txt", nil)
	var omit atomic.Bool
	_, _, web := syncServer(t, s, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if omit.Load() && r.URL.Path == "/v1/sync/changes" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"changes":[]}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	s.SyncNow()
	awaitSync(t, s)
	s.SetSyncPaused(true)
	before := s.State()
	omit.Store(true)
	remote, _ := vs.NewRemote(web.URL, web.Client(), false)
	if err := s.freshDownload(remote); Code(err) != "SYNC_DOWNLOAD_INCOMPLETE" {
		t.Fatal(err)
	}
	if after := s.State(); !after.Unlocked || after.Config.StorePath != before.Config.StorePath {
		t.Fatal("incomplete copy replaced current library")
	}
}

func TestFreshDownloadFinalSaveFailureRollsBackSelection(t *testing.T) {
	s, _ := testService(t)
	put(t, s, "keep.txt", nil)
	var failSave atomic.Bool
	badPath := filepath.Join(t.TempDir(), "missing", "settings.json")
	_, _, web := syncServer(t, s, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if failSave.Load() && r.URL.Path == "/v1/sync/changes" {
				s.mu.Lock()
				s.configPath = badPath
				s.mu.Unlock()
			}
			next.ServeHTTP(w, r)
		})
	})
	s.SyncNow()
	awaitSync(t, s)
	s.SetSyncPaused(true)
	before := s.State()
	failSave.Store(true)
	remote, _ := vs.NewRemote(web.URL, web.Client(), false)
	if err := s.freshDownload(remote); err == nil {
		t.Fatal("final settings failure accepted")
	}
	after := s.State()
	if !after.Unlocked || after.Config.StorePath != before.Config.StorePath || after.Config.PreviousStorePath != before.Config.PreviousStorePath || after.Config.FreshDownload == nil {
		t.Fatal("failed config switch did not roll back")
	}
	if _, err := s.List(); err != nil {
		t.Fatal("original library unavailable", err)
	}
}
