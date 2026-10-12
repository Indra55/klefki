package desktop

import (
	"testing"
	"time"

	"klefki/core/syncwire"
)

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
