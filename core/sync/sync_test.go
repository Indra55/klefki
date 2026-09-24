package sync_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	vc "vault/core/crypto"
	vs "vault/core/sync"
	"vault/server"
)

type harness struct {
	root       string
	backend    *server.Server
	web        *httptest.Server
	account    vc.Account
	master     vc.Key
	credential vc.Key
}

func setup(t *testing.T) *harness {
	t.Helper()
	h := &harness{root: t.TempDir()}
	var e error
	h.account, h.master, _, e = vc.NewAccount([]byte("M3 public synthetic password"))
	if e != nil {
		t.Fatal(e)
	}
	k, e := vc.Derive([]byte("M3 public synthetic password"), h.account.Descriptor)
	if e != nil {
		t.Fatal(e)
	}
	h.credential = vc.Credential(k.Auth, h.account.ID)
	k.Clear()
	h.start(t)
	t.Cleanup(func() { h.web.Close(); h.backend.Close() })
	return h
}
func (h *harness) start(t *testing.T) {
	t.Helper()
	var e error
	h.backend, e = server.Open(server.Config{DataDir: filepath.Join(h.root, "server"), RateBurst: 10000})
	if e != nil {
		t.Fatal(e)
	}
	if h.web == nil {
		h.web = httptest.NewTLSServer(h.backend)
	} else {
		addr := h.web.Listener.Addr().String()
		listener, e := net.Listen("tcp", addr)
		if e != nil {
			t.Fatal(e)
		}
		h.web = httptest.NewUnstartedServer(h.backend)
		h.web.Listener.Close()
		h.web.Listener = listener
		h.web.StartTLS()
	}
}
func (h *harness) device(t *testing.T, name string, signup bool) (*vs.Store, *vs.Client) {
	t.Helper()
	s, e := vs.Open(filepath.Join(h.root, name, "local.db"), h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	remote, e := vs.NewRemote(h.web.URL, h.web.Client(), false)
	if e != nil {
		t.Fatal(e)
	}
	var session vs.Session
	if signup {
		session, e = remote.Signup(context.Background(), h.account, h.credential)
	} else {
		session, e = remote.Login(context.Background(), h.account.ID, h.credential)
	}
	if e != nil {
		t.Fatal(e)
	}
	c := &vs.Client{Store: s, Remote: remote, Session: session, Master: h.master, Retry: vs.Retry{Attempts: 3, Base: time.Millisecond, Max: time.Millisecond}}
	return s, c
}
func add(t *testing.T, s *vs.Store, key vc.Key, object, base, name, body string) string {
	t.Helper()
	rev, e := s.Put(key, object, base, vc.Metadata{Type: "file", Name: name, Tags: []string{"M3 private tag"}, Size: uint64(len(body))}, bytes.NewBufferString(body))
	if e != nil {
		t.Fatal(e)
	}
	return rev
}
func syncNow(t *testing.T, c *vs.Client) {
	t.Helper()
	if e := c.Sync(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func content(t *testing.T, s *vs.Store, key vc.Key, revision string) string {
	t.Helper()
	var b bytes.Buffer
	if e := s.Export(key, revision, &b); e != nil {
		t.Fatal(e)
	}
	return b.String()
}
func TestOfflineConflictAndRestart(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "a", true)
	b, cb := h.device(t, "b", false)
	id, _ := vc.RandomID()
	object := id.String()
	first := add(t, a, h.master, object, "", "M3 private filename.pdf", "M3 original body")
	syncNow(t, ca)
	syncNow(t, cb)
	if content(t, b, h.master, first) != "M3 original body" {
		t.Fatal("initial pull lost content")
	}
	left := add(t, a, h.master, object, first, "M3 private filename.pdf", "M3 offline left body")
	right := add(t, b, h.master, object, first, "M3 private filename.pdf", "M3 offline right body")
	// Reopen a device while its offline edit remains queued.
	a.Close()
	var e error
	a, e = vs.Open(filepath.Join(h.root, "a", "local.db"), h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ca.Store = a
	syncNow(t, ca)
	syncNow(t, cb)
	syncNow(t, ca)
	for _, s := range []*vs.Store{a, b} {
		heads, e := s.Heads(h.master)
		if e != nil || len(heads) != 2 || !heads[0].Conflict || !heads[1].Conflict {
			t.Fatal("conflict copies missing")
		}
		if content(t, s, h.master, left) != "M3 offline left body" || content(t, s, h.master, right) != "M3 offline right body" {
			t.Fatal("conflict body lost")
		}
		if content(t, s, h.master, first) != "M3 original body" {
			t.Fatal("history overwritten")
		}
	}
	// The listener is kept while the actual server/database is reopened.
	h.web.Close()
	h.backend.Close()
	h.start(t)
	for _, c := range []*vs.Client{ca, cb} {
		r, e := vs.NewRemote(h.web.URL, h.web.Client(), false)
		if e != nil {
			t.Fatal(e)
		}
		c.Remote = r
	}
	syncNow(t, ca)
	syncNow(t, cb)
	n, e := a.Pending()
	if e != nil || n != 0 {
		t.Fatal("queue did not drain")
	}
	scan(t, h.root, [][]byte{[]byte("M3 private filename.pdf"), []byte("M3 private tag"), []byte("M3 original body"), []byte("M3 offline left body"), []byte("M3 offline right body"), []byte("M3 public synthetic password"), h.master[:], h.credential[:], []byte(ca.Session.Token)})
}
func scan(t *testing.T, root string, needles [][]byte) {
	t.Helper()
	if e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		for _, n := range needles {
			for _, v := range [][]byte{n, []byte(hex.EncodeToString(n)), []byte(base64.StdEncoding.EncodeToString(n))} {
				if bytes.Contains(b, v) {
					t.Fatal("plaintext or secret persisted")
				}
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRetryLostCommitAndInterruptedUpload(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "a", true)
	b, cb := h.device(t, "b", false)
	id, _ := vc.RandomID()
	body := string(bytes.Repeat([]byte("synthetic M3 data"), 300000))
	rev := add(t, a, h.master, id.String(), "", "retry.pdf", body)
	transport := h.web.Client().Transport
	var lost, failed atomic.Bool
	var attempts atomic.Int32
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "PUT" && r.URL.Path == "/v1/sync/revisions/"+rev && !lost.Swap(true) {
			res, e := transport.RoundTrip(r)
			if e != nil {
				return nil, e
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			return nil, io.ErrUnexpectedEOF
		}
		if r.Method == "PUT" && bytes.HasSuffix([]byte(r.URL.Path), []byte("/chunks/1")) {
			attempts.Add(1)
			if !failed.Swap(true) {
				return nil, io.ErrUnexpectedEOF
			}
		}
		return transport.RoundTrip(r)
	})}
	var e error
	ca.Remote, e = vs.NewRemote(h.web.URL, client, false)
	if e != nil {
		t.Fatal(e)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	if attempts.Load() < 2 || !lost.Load() || content(t, b, h.master, rev) != body {
		t.Fatal("retry lost data")
	}
	heads, e := b.Heads(h.master)
	if e != nil || len(heads) != 1 {
		t.Fatal("lost response duplicated revision")
	}
}
func TestOfflineQueueAndAuthenticationFailure(t *testing.T) {
	h := setup(t)
	s, c := h.device(t, "a", true)
	id, _ := vc.RandomID()
	rev := add(t, s, h.master, id.String(), "", "offline.pdf", "offline body")
	var calls atomic.Int32
	r, e := vs.NewRemote(h.web.URL, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, io.ErrUnexpectedEOF })}, false)
	if e != nil {
		t.Fatal(e)
	}
	original := c.Remote
	c.Remote = r
	if c.Sync(context.Background()) == nil || calls.Load() != 3 {
		t.Fatal("bounded network retry missing")
	}
	if n, _ := s.Pending(); n != 1 {
		t.Fatal("offline queue lost")
	}
	c.Remote = original
	c.Session.Token = "invalid"
	if c.Sync(context.Background()) == nil {
		t.Fatal("bad auth accepted")
	}
	if content(t, s, h.master, rev) != "offline body" {
		t.Fatal("authentication failure damaged offline data")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Sync(ctx) == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestLocalAtomicityAndWrongKey(t *testing.T) {
	h := setup(t)
	s, _ := h.device(t, "a", true)
	id, _ := vc.RandomID()
	_, e := s.Put(h.master, id.String(), "", vc.Metadata{Type: "file", Name: "secret partial filename", Tags: []string{}, Size: 10}, bytes.NewBufferString("short"))
	if e == nil {
		t.Fatal("short input accepted")
	}
	if n, _ := s.Pending(); n != 0 {
		t.Fatal("partial edit queued")
	}
	rev := add(t, s, h.master, id.String(), "", "name", "complete")
	wrong, _ := vc.RandomKey()
	var out bytes.Buffer
	if s.Export(wrong, rev, &out) == nil || out.Len() != 0 {
		t.Fatal("wrong key released content")
	}
	other, _ := vc.RandomID()
	if _, e := vs.Open(filepath.Join(h.root, "a", "local.db"), other); e == nil {
		t.Fatal("cross-account local database accepted")
	}
}

func TestInterruptedSyncRestartsAndSkipsAcceptedChunks(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "a", true)
	b, cb := h.device(t, "b", false)
	id, _ := vc.RandomID()
	body := string(bytes.Repeat([]byte("M3 resume fixture "), 260000))
	rev := add(t, a, h.master, id.String(), "", "resumed.pdf", body)
	transport := h.web.Client().Transport
	var firstUploads atomic.Int32
	broken := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/chunks/0") {
			firstUploads.Add(1)
		}
		if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/chunks/1") {
			return nil, io.ErrUnexpectedEOF
		}
		return transport.RoundTrip(r)
	})}
	var e error
	ca.Remote, e = vs.NewRemote(h.web.URL, broken, false)
	if e != nil {
		t.Fatal(e)
	}
	if ca.Sync(context.Background()) == nil {
		t.Fatal("interruption not observed")
	}
	if n, _ := a.Pending(); n != 1 {
		t.Fatal("interruption removed pending change")
	}
	h.web.Close()
	h.backend.Close()
	h.start(t)
	a.Close()
	a, e = vs.Open(filepath.Join(h.root, "a", "local.db"), h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	ca.Store = a
	transport = h.web.Client().Transport
	resumed := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/chunks/0") {
			firstUploads.Add(1)
		}
		return transport.RoundTrip(r)
	})}
	ca.Remote, e = vs.NewRemote(h.web.URL, resumed, false)
	if e != nil {
		t.Fatal(e)
	}
	cb.Remote, e = vs.NewRemote(h.web.URL, h.web.Client(), false)
	if e != nil {
		t.Fatal(e)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	if firstUploads.Load() != 1 {
		t.Fatal("resume uploaded an already accepted chunk")
	}
	if content(t, b, h.master, rev) != body {
		t.Fatal("resume lost content")
	}
}
func TestCorruptDownloadDoesNotAdvanceCursor(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "a", true)
	b, cb := h.device(t, "b", false)
	id, _ := vc.RandomID()
	rev := add(t, a, h.master, id.String(), "", "authenticated.pdf", "authenticated synthetic body")
	syncNow(t, ca)
	transport := h.web.Client().Transport
	bad := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		res, e := transport.RoundTrip(r)
		if e != nil {
			return nil, e
		}
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/chunks/") {
			data, e := io.ReadAll(res.Body)
			res.Body.Close()
			if e != nil {
				return nil, e
			}
			data[len(data)-1] ^= 1
			res.Body = io.NopCloser(bytes.NewReader(data))
		}
		return res, nil
	})}
	var e error
	cb.Remote, e = vs.NewRemote(h.web.URL, bad, false)
	if e != nil {
		t.Fatal(e)
	}
	if cb.Sync(context.Background()) == nil {
		t.Fatal("corrupt chunk accepted")
	}
	if n, _ := b.Cursor(); n != 0 {
		t.Fatal("cursor advanced past corrupt file")
	}
	if heads, _ := b.Heads(h.master); len(heads) != 0 {
		t.Fatal("partial revision visible")
	}
	cb.Remote, e = vs.NewRemote(h.web.URL, h.web.Client(), false)
	if e != nil {
		t.Fatal(e)
	}
	syncNow(t, cb)
	if content(t, b, h.master, rev) != "authenticated synthetic body" {
		t.Fatal("failed pull could not recover")
	}
}
func TestQueuedChainPaginationAndEmptyFiles(t *testing.T) {
	h := setup(t)
	a, ca := h.device(t, "a", true)
	b, cb := h.device(t, "b", false)
	id, _ := vc.RandomID()
	object := id.String()
	base := ""
	revisions := []string{}
	for i := 0; i < 18; i++ {
		base = add(t, a, h.master, object, base, "chain.pdf", "")
		revisions = append(revisions, base)
	}
	folder, _ := vc.RandomID()
	if _, e := a.Put(h.master, folder.String(), "", vc.Metadata{Type: "folder", Name: "private folder", Tags: []string{}}, nil); e != nil {
		t.Fatal(e)
	}
	syncNow(t, ca)
	syncNow(t, cb)
	heads, e := b.Heads(h.master)
	if e != nil || len(heads) != 2 {
		t.Fatal("pagination/chain heads")
	}
	for _, r := range revisions {
		if content(t, b, h.master, r) != "" {
			t.Fatal("empty history changed")
		}
	}
}
func TestRemotePolicyAndRedirect(t *testing.T) {
	for _, url := range []string{"http://example.com", "http://127.0.0.1", "https://user:secret@example.com", "https://example.com/path"} {
		if _, e := vs.NewRemote(url, nil, false); e == nil {
			t.Fatal("unsafe remote accepted")
		}
	}
	var received atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Store(true) }))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	remote, e := vs.NewRemote(redirect.URL, redirect.Client(), false)
	if e != nil {
		t.Fatal(e)
	}
	id, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	if _, e = remote.Login(context.Background(), id, key); e == nil || received.Load() {
		t.Fatal("redirect followed")
	}
}
