package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

func TestDeviceStatePersistenceOrderingAndCompatibility(t *testing.T) {
	now := time.Now()
	config := Config{DataDir: t.TempDir(), RateBurst: 10000, Now: func() time.Time { return now }}
	s, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ts := httptest.NewTLSServer(s)
	f := newFixture(t)
	session := enroll(t, ts.Client(), ts.URL, f)
	other := enroll(t, ts.Client(), ts.URL, newFixture(t))
	ts.Close()
	id, _ := vc.RandomID()
	device := id.String()
	w := statusRequest(s, session.Token, device, "POST", "/v1/sync/relay/device", syncwire.RelayDevice{Device: device, Known: []string{}})
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	read := func() syncwire.Status {
		t.Helper()
		w := statusRequest(s, session.Token, device, "GET", "/v1/sync/status?deviceState=1", nil)
		var status syncwire.Status
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || !status.DeviceStateSupported || len(status.Devices) != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
		return status
	}
	if read().Devices[0].DeviceState != nil {
		t.Fatal("missing report must be unknown")
	}
	update := syncwire.DeviceStateUpdate{Device: device, Version: 2, Pending: true}
	post := func(token, header string, body any, code int) {
		t.Helper()
		w := statusRequest(s, token, header, "POST", "/v1/sync/device-state", body)
		if w.Code != code {
			t.Fatalf("want %d, got %d: %s", code, w.Code, w.Body.String())
		}
	}
	post(other.Token, device, update, 410)
	post(session.Token, session.Device, update, 400)
	post(session.Token, device, map[string]any{"device": device, "version": 1}, 400)
	post(session.Token, device, syncwire.DeviceStateUpdate{Device: device, Version: syncwire.MaxDeviceStateVersion + 1}, 400)
	post(session.Token, device, update, 200)
	saved := *read().Devices[0].DeviceState
	var before, after int
	s.db.QueryRow(`SELECT total_changes()`).Scan(&before)
	now = now.Add(time.Minute)
	post(session.Token, device, update, 200)
	s.db.QueryRow(`SELECT total_changes()`).Scan(&after)
	if before != after || *read().Devices[0].DeviceState != saved {
		t.Fatal("duplicate report rewrote the record")
	}
	post(session.Token, device, syncwire.DeviceStateUpdate{Device: device, Version: 1, Pending: false}, 409)
	post(session.Token, device, syncwire.DeviceStateUpdate{Device: device, Version: 2, Pending: false}, 409)
	// A status request from an old strict-decoding client must retain its original shape.
	legacy := statusRequest(s, session.Token, device, "GET", "/v1/sync/status", nil)
	if legacy.Code != 200 || strings.Contains(legacy.Body.String(), "deviceState") {
		t.Fatal("legacy status shape changed", legacy.Body.String())
	}
	foreign := statusRequest(s, other.Token, device, "GET", "/v1/sync/status?deviceState=1", nil)
	if strings.Contains(foreign.Body.String(), device) {
		t.Fatal("cross-account report leaked")
	}
	// Reports remain available after transient presence expires and across database reopen.
	s.Close()
	s, err = Open(config)
	if err != nil {
		t.Fatal(err)
	}
	status := read()
	if status.Devices[0].Phase != "unknown" || *status.Devices[0].DeviceState != saved {
		t.Fatal("restart lost pending report", status)
	}
	unlock := s.relayLocks.lock(f.account.ID.String())
	done := make(chan int, 1)
	go func() {
		done <- statusRequest(s, session.Token, device, "POST", "/v1/sync/device-state", syncwire.DeviceStateUpdate{Device: device, Version: 3, Pending: false}).Code
	}()
	select {
	case code := <-done:
		if code != 200 {
			t.Error(code)
		}
	case <-time.After(time.Second):
		t.Error("report blocked by document transfer")
	}
	unlock()
	if read().Devices[0].DeviceState.Pending {
		t.Fatal("new clean report did not clear warning")
	}
	// Forgetting a device also removes its bounded status record.
	self, _ := vc.RandomID()
	statusRequest(s, session.Token, self.String(), "POST", "/v1/sync/relay/device", syncwire.RelayDevice{Device: self.String(), Known: []string{}})
	w = statusRequest(s, session.Token, self.String(), "DELETE", "/v1/sync/relay/devices/"+device, nil)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	var count int
	if s.db.QueryRow(`SELECT COUNT(*) FROM sync_device_state`).Scan(&count) != nil || count != 0 {
		t.Fatal("forgotten report retained", count)
	}
	post(session.Token, device, update, 410)
}
