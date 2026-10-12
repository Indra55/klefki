package desktop

import (
	"net/http"
	"testing"
	"time"

	vs "klefki/core/sync"
	"klefki/core/syncwire"
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
