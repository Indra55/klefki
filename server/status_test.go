package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

func statusRequest(s *Server, token, device, method, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "https://example.test"+path, bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Klefki-Device", device)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestDevicePresenceReceiptsAndIsolation(t *testing.T) {
	now := time.Now()
	s, err := Open(Config{DataDir: t.TempDir(), RateBurst: 10000, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewTLSServer(s)
	defer ts.Close()
	f := newFixture(t)
	session := enroll(t, ts.Client(), ts.URL, f)
	other := enroll(t, ts.Client(), ts.URL, newFixture(t))
	a, _ := vc.RandomID()
	b, _ := vc.RandomID()
	for _, id := range []string{a.String(), b.String()} {
		w := statusRequest(s, session.Token, id, "POST", "/v1/sync/relay/device", syncwire.RelayDevice{Device: id, Known: []string{}})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	in := syncwire.Presence{Device: a.String(), ClientKind: "desktop", Phase: "uploading", DoneBytes: 12, TotalBytes: 24}
	if w := statusRequest(s, other.Token, a.String(), "POST", "/v1/sync/presence", in); w.Code != 410 {
		t.Fatal("cross-account presence accepted", w.Code)
	}
	if w := statusRequest(s, session.Token, b.String(), "POST", "/v1/sync/presence", in); w.Code != 400 {
		t.Fatal("mismatched identity accepted", w.Code)
	}
	if w := statusRequest(s, session.Token, a.String(), "POST", "/v1/sync/presence", in); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	object, _ := vc.RandomID()
	rev, _ := vc.RandomID()
	metadata, _ := vc.EncryptMetadata(f.master, f.account.ID, object, vc.Metadata{Type: "folder", Name: "private folder", Tags: []string{}})
	r := syncwire.Revision{ID: rev.String(), Object: object.String(), Metadata: metadata}
	request(t, ts.Client(), ts.URL, "PUT", "/v1/sync/revisions/"+r.ID, session.Token, r, 201)
	var out syncwire.Status
	read := func(token string) syncwire.Status {
		var out syncwire.Status
		w := statusRequest(s, token, a.String(), "GET", "/v1/sync/status", nil)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return out
	}
	out = read(session.Token)
	if len(out.Devices) != 2 || out.Devices[0].Outstanding != 1 || out.Devices[1].Outstanding != 1 {
		t.Fatal("missing outstanding receipts", out)
	}
	for _, d := range out.Devices {
		if d.Device == a.String() && (d.Phase != "uploading" || d.DoneBytes != 12) {
			t.Fatal("missing live progress", d)
		}
	}
	if foreign := read(other.Token); len(foreign.Devices) != 0 || foreign.LatestSequence != 0 {
		t.Fatal("cross-account status leak", foreign)
	}
	ack := syncwire.Cursor{Sequence: out.LatestSequence}
	if w := statusRequest(s, session.Token, b.String(), "PUT", "/v1/sync/cursor", ack); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, d := range read(session.Token).Devices {
		if d.Device == b.String() && d.Outstanding != 0 {
			t.Fatal("durable receipt missing", d)
		}
	}
	now = now.Add(46 * time.Second)
	for _, d := range read(session.Token).Devices {
		if d.Phase != "unknown" {
			t.Fatal("stale device still live", d)
		}
	}
	// Status must stay responsive while a transfer owns the global relay lock.
	s.relayMu.Lock()
	finished := make(chan int, 1)
	go func() { finished <- statusRequest(s, session.Token, a.String(), "GET", "/v1/sync/status", nil).Code }()
	select {
	case code := <-finished:
		if code != 200 {
			t.Error(code)
		}
	case <-time.After(time.Second):
		t.Error("status blocked behind relay transfer")
	}
	s.relayMu.Unlock()
	if w := statusRequest(s, session.Token, a.String(), "DELETE", "/v1/sync/relay/devices/"+b.String(), nil); w.Code != 204 {
		t.Fatal(w.Code)
	}
	in.Device = b.String()
	if w := statusRequest(s, session.Token, b.String(), "POST", "/v1/sync/presence", in); w.Code != 410 {
		t.Fatal("forgotten device accepted", w.Code)
	}
}

func TestProvenancePreservesOriginAndLegacyResponses(t *testing.T) {
	s, err := Open(Config{DataDir: t.TempDir(), RateBurst: 10000})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewTLSServer(s)
	defer ts.Close()
	f := newFixture(t)
	session := enroll(t, ts.Client(), ts.URL, f)
	a, _ := vc.RandomID()
	b, _ := vc.RandomID()
	object, _ := vc.RandomID()
	id, _ := vc.RandomID()
	for _, device := range []string{a.String(), b.String()} {
		if w := statusRequest(s, session.Token, device, "POST", "/v1/sync/relay/device", syncwire.RelayDevice{Device: device, Known: []string{}}); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	m, _ := vc.EncryptMetadata(f.master, f.account.ID, object, vc.Metadata{Type: "folder", Name: "source fixture", Tags: []string{}})
	revision := syncwire.Revision{ID: id.String(), Object: object.String(), Metadata: m}
	put := func(device, origin string, want int) {
		body, _ := json.Marshal(revision)
		r := httptest.NewRequest(http.MethodPut, "https://example.test/v1/sync/revisions/"+id.String(), bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+session.Token)
		r.Header.Set("X-Klefki-Device", device)
		r.Header.Set("X-Klefki-Origin-Device", origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	put(a.String(), a.String(), 201)
	put(b.String(), b.String(), 200)
	w := statusRequest(s, session.Token, a.String(), "GET", "/v1/sync/provenance?after=0", nil)
	var provenance syncwire.ProvenancePage
	if json.Unmarshal(w.Body.Bytes(), &provenance) != nil || len(provenance.Changes) != 1 || provenance.Changes[0].Origin != a.String() || provenance.Changes[0].Publisher != b.String() {
		t.Fatal("origin changed on republish", w.Body.String())
	}
	w = statusRequest(s, session.Token, a.String(), "GET", "/v1/sync/changes?after=0", nil)
	d := json.NewDecoder(w.Body)
	d.DisallowUnknownFields()
	var legacy syncwire.Page
	if d.Decode(&legacy) != nil || len(legacy.Changes) != 1 {
		t.Fatal("legacy response changed")
	}
}
