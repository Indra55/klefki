package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

func TestWorkFeedIsScopedBoundedAndExcludesOwnSignals(t *testing.T) {
	backend, err := Open(Config{DataDir: t.TempDir(), RateBurst: 1000})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	web := httptest.NewTLSServer(backend)
	defer web.Close()
	fixture := newFixture(t)
	session := enroll(t, web.Client(), web.URL, fixture)
	device, _ := vc.RandomID()
	other, _ := vc.RandomID()
	file, _ := vc.RandomID()
	foreign, _ := vc.RandomID()
	for _, id := range []string{device.String(), other.String()} {
		if _, err = backend.db.Exec(`INSERT INTO relay_devices(account,device,sequence) VALUES(?,?,0)`, fixture.account.ID.String(), id); err != nil {
			t.Fatal(err)
		}
	}
	var latest int64
	for i := 0; i < 270; i++ {
		result, err := backend.db.Exec(`INSERT INTO content_events(account,device,file) VALUES(?,?,?)`, fixture.account.ID.String(), other.String(), file.String())
		if err != nil {
			t.Fatal(err)
		}
		latest, _ = result.LastInsertId()
	}
	backend.db.Exec(`INSERT INTO content_events(account,device,file) VALUES(?,?,?)`, fixture.account.ID.String(), device.String(), foreign.String())
	backend.db.Exec(`INSERT INTO content_events(account,device,file) VALUES(?,?,?)`, foreign.String(), other.String(), foreign.String())
	get := func(path string, want int) []byte {
		request, err := http.NewRequest("GET", web.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+session.Token)
		request.Header.Set("X-Klefki-Content", "1")
		request.Header.Set("X-Klefki-Deletion", "1")
		request.Header.Set("X-Klefki-Device", device.String())
		response, err := web.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("%s: status %d", path, response.StatusCode)
		}
		body, _ := io.ReadAll(response.Body)
		return body
	}
	var check syncwire.WorkCheck
	if err = json.Unmarshal(get("/v1/sync/check", 200), &check); err != nil || check.Content != latest {
		t.Fatal("check included own/foreign signals", check, err)
	}
	var page syncwire.ContentEvents
	if err = json.Unmarshal(get("/v1/sync/content/changes?after=0", 200), &page); err != nil || len(page.Changes) != 256 || page.Latest != latest {
		t.Fatal("unbounded content feed", len(page.Changes), page.Latest, err)
	}
	for _, event := range page.Changes {
		if event.File != file.String() {
			t.Fatal("foreign file leaked")
		}
	}
	cursor := page.Changes[len(page.Changes)-1].Sequence
	if err = json.Unmarshal(get(fmt.Sprintf("/v1/sync/content/changes?after=%d&through=%d", cursor, latest), 200), &page); err != nil || len(page.Changes) != 14 {
		t.Fatal("content pagination lost events", len(page.Changes), err)
	}
	get("/v1/sync/content/changes?after=-1", 400)
	get(fmt.Sprintf("/v1/sync/content/changes?after=%d", latest+1), 409)
	request(t, web.Client(), web.URL, "GET", "/v1/sync/check", "", nil, 401)
}
