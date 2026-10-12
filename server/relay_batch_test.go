package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

func TestRelayBatchLimitsAndLegacyShape(t *testing.T) {
	s, e := Open(Config{DataDir: t.TempDir(), RateBurst: 10000})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ts := httptest.NewTLSServer(s)
	defer ts.Close()
	f := newFixture(t)
	session := enroll(t, ts.Client(), ts.URL, f)
	device, _ := vc.RandomID()
	ids := make([]string, 257)
	for i := range ids {
		id, _ := vc.RandomID()
		ids[i] = id.String()
	}
	in := syncwire.RelayPrepare{RelayDevice: syncwire.RelayDevice{Device: device.String(), Known: []string{}}, IDs: ids[:256]}
	w := statusRequest(s, session.Token, device.String(), "POST", "/v1/sync/relay/prepare", in)
	var out syncwire.RelayPrepared
	if w.Code != 200 || w.Body.Len() > 16384 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.IDs) != 256 {
		t.Fatalf("prepare %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/v1/sync/relay/prepare", "/v1/sync/relay/need-batch"} {
		var body any = syncwire.RelayNeed{IDs: ids}
		if strings.HasSuffix(path, "prepare") {
			in.IDs = ids
			body = in
		}
		if w := statusRequest(s, session.Token, device.String(), "POST", path, body); w.Code != 400 {
			t.Fatalf("257 IDs: %s %d", path, w.Code)
		}
		if w := statusRequest(s, session.Token, device.String(), "POST", path, map[string]string{"padding": strings.Repeat("x", 16384)}); w.Code != 413 {
			t.Fatalf("body cap: %s %d", path, w.Code)
		}
	}
	w = statusRequest(s, session.Token, device.String(), "POST", "/v1/sync/relay/device", in.RelayDevice)
	var legacy syncwire.RelayState
	decoder := json.NewDecoder(w.Body)
	decoder.DisallowUnknownFields()
	if w.Code != 200 || decoder.Decode(&legacy) != nil {
		t.Fatal("legacy enrollment shape changed")
	}
	if w := statusRequest(s, session.Token, device.String(), "POST", "/v1/sync/relay/need", syncwire.RelayNeed{IDs: ids[:17]}); w.Code != 400 {
		t.Fatal("legacy page limit changed")
	}
}
func TestConcurrentRelayQuota(t *testing.T) {
	s, e := Open(Config{DataDir: t.TempDir(), RateBurst: 10000, RelayMaxBytes: 400})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ts := httptest.NewTLSServer(s)
	defer ts.Close()
	f := newFixture(t)
	session := enroll(t, ts.Client(), ts.URL, f)
	device, _ := vc.RandomID()
	if w := statusRequest(s, session.Token, device.String(), "POST", "/v1/sync/relay/device", syncwire.RelayDevice{Device: device.String()}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := statusRequest(s, session.Token, device.String(), "PUT", "/v1/sync/settings", map[string]string{"mode": syncwire.TemporaryRelay}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 4)
	for i := 0; i < 4; i++ {
		file, _ := vc.RandomID()
		key, _ := vc.RandomKey()
		chunk, e := vc.Seal(key, vc.Context{Kind: vc.Chunk, Account: f.account.ID, Object: file, Final: true}, bytes.Repeat([]byte("x"), 200), nil)
		if e != nil {
			t.Fatal(e)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("PUT", "https://example.test/v1/blobs/"+file.String()+"/chunks/0", bytes.NewReader(chunk))
			r.Header.Set("Authorization", "Bearer "+session.Token)
			r.Header.Set("X-Klefki-Device", device.String())
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	accepted := 0
	for code := range codes {
		if code == 201 {
			accepted++
		} else if code != 507 {
			t.Errorf("status %d", code)
		}
	}
	if accepted != 1 {
		t.Fatalf("quota overcommitted: accepted %d", accepted)
	}
}
