package desktop

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
	"klefki/server"
)

type stalledTransport struct{ started chan struct{} }

func (r stalledTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/v1/sync/settings" {
		close(r.started)
	}
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestLibraryAndEditsRemainAvailableDuringSync(t *testing.T) {
	s, _ := testService(t)
	rev := put(t, s, "available.txt", nil)
	started := make(chan struct{})
	r, err := vs.NewRemote("https://relay.example", &http.Client{Transport: stalledTransport{started}}, false)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config.Server = "https://relay.example"
	s.session.remote = r
	s.session.remoteSession = vs.Session{Token: "fixture"}
	s.mu.Unlock()
	s.SyncNow()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sync did not start")
	}
	done := make(chan error, 1)
	go func() {
		list, err := s.List()
		if err == nil && (len(list.Items) != 1 || list.Items[0].Revision != rev) {
			err = ErrInvalid
		}
		if err == nil {
			_, err = s.Folder("Added during sync", nil)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		s.mu.Lock()
		s.session.cancel()
		s.mu.Unlock()
		<-done
		t.Fatal("network sync blocked local library and edits")
	}
	s.Lock()
	if s.State().Unlocked {
		t.Fatal("sync prevented locking")
	}
}

func TestSyncStatusDoesNotWaitForLibraryAndClearsOnLock(t *testing.T) {
	s, _ := testService(t)
	s.mu.Lock()
	x := s.session
	s.mu.Unlock()
	s.work.Lock()
	done := make(chan struct{})
	go func() {
		s.observeSync(x, syncwire.Progress{Phase: "uploading", Name: "private filename", DoneBytes: 4, TotalBytes: 8})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		s.work.Unlock()
		t.Fatal("status waited for library lock")
	}
	if s.State().SyncInfo.Progress.Name != "private filename" {
		s.work.Unlock()
		t.Fatal("missing progress")
	}
	s.work.Unlock()
	id := s.State().SyncDevice
	if err := s.SetDeviceAlias(id, "My laptop"); err != nil {
		t.Fatal(err)
	}
	if s.State().DeviceAliases[id] != "My laptop" {
		t.Fatal("nickname missing")
	}
	s.Lock()
	s.observeSync(x, syncwire.Progress{Phase: "uploading", Name: "stale filename"})
	if st := s.State(); st.SyncInfo.Progress != nil || len(st.DeviceAliases) != 0 {
		t.Fatal("private sync state survived lock", st.SyncInfo)
	}
}

func syncServer(t *testing.T, s *Service, wrap func(http.Handler) http.Handler) (*session, *vs.Remote, *httptest.Server) {
	t.Helper()
	backend, err := server.Open(server.Config{DataDir: t.TempDir(), RateBurst: 10000})
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler = backend
	if wrap != nil {
		handler = wrap(handler)
	}
	web := httptest.NewTLSServer(handler)
	t.Cleanup(func() {
		s.Close()
		web.Close()
		backend.Close()
	})
	s.mu.Lock()
	x := s.session
	s.mu.Unlock()
	r, err := vs.NewRemote(web.URL, web.Client(), false)
	if err != nil {
		t.Fatal(err)
	}
	remoteSession, err := r.Signup(x.ctx, x.account, x.credential)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config.Server = web.URL
	x.remote, x.remoteSession = r, remoteSession
	s.mu.Unlock()
	return x, r, web
}

func awaitSync(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		running := s.syncRunning || s.syncRefresh
		s.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync did not finish")
}

func TestRefreshSyncRenewsExpiredSessionAndReceivesChanges(t *testing.T) {
	s, _ := testService(t)
	x, remote, web := syncServer(t, s, nil)
	s.SyncNow()
	awaitSync(t, s)
	before := s.State()
	if before.Sync != "Synced" {
		t.Fatal("initial sync", before.SyncInfo)
	}
	if err := s.SetDeviceAlias(before.SyncDevice, "This laptop"); err != nil {
		t.Fatal(err)
	}
	other, err := vs.Open(filepath.Join(t.TempDir(), "other.db"), x.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherRemote, err := vs.NewRemote(web.URL, web.Client(), false)
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := otherRemote.Login(x.ctx, x.account.ID, x.credential)
	if err != nil {
		t.Fatal(err)
	}
	c := vs.Client{Store: other, Remote: otherRemote, Session: otherSession, Master: x.master}
	defer clear(c.Master[:])
	object, _ := vc.RandomID()
	rev, err := other.Put(x.master, object.String(), "", vc.Metadata{Type: "file", Name: "incoming.txt", Tags: []string{}, Size: 5}, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Sync(x.ctx); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	oldSession := x.remoteSession
	s.mu.Unlock()
	if err = remote.Revoke(x.ctx, oldSession); err != nil {
		t.Fatal(err)
	}
	s.RefreshSync()
	awaitSync(t, s)
	after := s.State()
	if !after.Unlocked || after.Generation != before.Generation || after.Sync != "Synced" || after.SyncDevice != before.SyncDevice || after.DeviceAliases[before.SyncDevice] != "This laptop" {
		t.Fatal("refresh changed unlocked identity or failed", after.SyncInfo)
	}
	s.mu.Lock()
	renewed := x.remoteSession.Token != oldSession.Token
	s.mu.Unlock()
	if !renewed || find(t, s, rev).Name != "incoming.txt" {
		t.Fatal("refresh did not renew and receive")
	}
}

func TestRefreshSyncQueuedBehindFailureRunsWithoutLocalChanges(t *testing.T) {
	s, _ := testService(t)
	started, release := make(chan struct{}), make(chan struct{}, 1)
	defer close(release)
	var blocked atomic.Bool
	var logins atomic.Int32
	syncServer(t, s, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/login" {
				logins.Add(1)
			}
			if r.URL.Path == "/v1/sync/deletions" && blocked.CompareAndSwap(false, true) {
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	s.SyncNow()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not reach blocker")
	}
	s.RefreshSync()
	s.RefreshSync()
	release <- struct{}{}
	awaitSync(t, s)
	if st := s.State(); !st.Unlocked || st.Pending != 0 || st.Sync != "Synced" || logins.Load() != 1 {
		t.Fatal("queued refresh lost or duplicated", st.SyncInfo, logins.Load())
	}
}

func TestStatusPollCannotCompleteSourceWait(t *testing.T) {
	s, _ := testService(t)
	syncServer(t, s, nil)
	s.SyncNow()
	awaitSync(t, s)
	s.mu.Lock()
	s.syncInfo.Phase, s.syncInfo.WaitingFor = "waiting", "source"
	s.syncInfo.LastDeviceCheck = 0
	s.mu.Unlock()
	s.SetSyncPanel(true)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info := s.State().SyncInfo
		if info.LastDeviceCheck > 0 {
			if info.Phase != "waiting" || info.WaitingFor != "source" {
				t.Fatal("status poll incorrectly completed source wait", info)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("status poll did not run")
}

func TestSyncPauseKeepsLocalChangesAcrossRestartAndResumes(t *testing.T) {
	s, _ := testService(t)
	var requests atomic.Int32
	x, _, _ := syncServer(t, s, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			next.ServeHTTP(w, r)
		})
	})
	if err := s.SetSyncPaused(true); err != nil {
		t.Fatal(err)
	}
	requests.Store(0)
	for _, name := range []string{"Batch one", "Batch two"} {
		if _, err := s.Folder(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	s.SyncNow()
	s.RefreshSync()
	s.syncNow(true)
	s.SetSyncPanel(true)
	time.Sleep(1800 * time.Millisecond)
	if requests.Load() != 0 || s.State().Pending != 2 || !s.State().SyncInfo.Paused {
		t.Fatal("paused client transferred or lost changes", requests.Load(), s.State().SyncInfo)
	}
	// A new process must load the preference before its unlock-triggered sync.
	reopened, err := New(filepath.Dir(s.configPath), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.State().Config.SyncPaused {
		t.Fatal("pause was not persisted")
	}
	reopened.Close()
	s.Lock()
	if err := s.Unlock("synthetic desktop password", false); err != nil {
		t.Fatal(err)
	}
	if !s.State().SyncInfo.Paused {
		t.Fatal("unlock cleared pause")
	}
	// Restore the fixture's trusted TLS transport after unlocking.
	s.mu.Lock()
	s.session.remote = x.remote
	s.mu.Unlock()
	if err := s.SetSyncPaused(false); err != nil {
		t.Fatal(err)
	}
	awaitSync(t, s)
	if st := s.State(); st.Pending != 0 || st.SyncInfo.Paused || st.Sync != "Synced" {
		t.Fatal("resume did not upload the local batch", st.SyncInfo)
	}
}

func TestSyncPauseFinishesCurrentCycleWithoutQueuedRefresh(t *testing.T) {
	s, _ := testService(t)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var blocked atomic.Bool
	var logins atomic.Int32
	syncServer(t, s, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/login" {
				logins.Add(1)
			}
			if r.URL.Path == "/v1/sync/deletions" && blocked.CompareAndSwap(false, true) {
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	s.SyncNow()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not start")
	}
	s.RefreshSync()
	if err := s.SetSyncPaused(true); err != nil {
		t.Fatal(err)
	}
	if !s.State().SyncInfo.Paused || !s.State().SyncInfo.Running {
		t.Fatal("missing finishing state")
	}
	release <- struct{}{}
	awaitSync(t, s)
	if st := s.State(); !st.SyncInfo.Paused || st.SyncInfo.Running || logins.Load() != 0 {
		t.Fatal("queued refresh ran after pausing", st.SyncInfo)
	}
}

func TestDeviceStateMonitorReportsEditsDespiteFailedUpload(t *testing.T) {
	s, _ := testService(t)
	var blocked atomic.Bool
	x, _, _ := syncServer(t, s, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if blocked.Load() && strings.HasPrefix(r.URL.Path, "/v1/sync/revisions/") {
				http.Error(w, "fixture upload blocked", 507)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	s.SyncNow()
	awaitSync(t, s)
	waitReport := func(pending bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			st := s.State()
			if st.SyncInfo.Peers != nil {
				for _, d := range st.SyncInfo.Peers.Devices {
					if d.Device == st.SyncDevice && d.DeviceState != nil && d.DeviceState.Pending == pending {
						return
					}
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("monitor did not publish pending state", pending, s.State().SyncInfo)
	}
	waitReport(false)
	blocked.Store(true)
	if _, err := s.Folder("Saved while upload fails", nil); err != nil {
		t.Fatal(err)
	}
	waitReport(true)
	s.SyncNow()
	awaitSync(t, s)
	if s.State().Pending != 1 || s.State().SyncInfo.Phase != "full" {
		t.Fatal("failed publication lost pending work", s.State().SyncInfo)
	}
	if n, err := x.store.Pending(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	blocked.Store(false)
	s.SyncNow()
	awaitSync(t, s)
	waitReport(false)
}
