package server

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	vc "klefki/core/crypto"
)

type fixture struct {
	account            vc.Account
	master, credential vc.Key
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	a, m, _, e := vc.NewAccount([]byte("synthetic M2 password never persisted"))
	if e != nil {
		t.Fatal(e)
	}
	k, e := vc.Derive([]byte("synthetic M2 password never persisted"), a.Descriptor)
	if e != nil {
		t.Fatal(e)
	}
	defer k.Clear()
	return fixture{a, m, vc.Credential(k.Auth, a.ID)}
}
func hexID(id vc.ID) string { return hex.EncodeToString(id[:]) }
func request(t *testing.T, c *http.Client, base, method, path, token string, body any, want int) []byte {
	t.Helper()
	var r io.Reader
	if b, ok := body.([]byte); ok {
		r = bytes.NewReader(b)
	} else if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		r = bytes.NewReader(b)
	}
	req, e := http.NewRequest(method, base+path, r)
	if e != nil {
		t.Fatal(e)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := c.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s: status %d want %d", method, path, res.StatusCode, want)
	}
	return b
}
func enroll(t *testing.T, c *http.Client, url string, f fixture) Session {
	b := request(t, c, url, "POST", "/v1/signup", "", Signup{Account: hexID(f.account.ID), Descriptor: f.account.Descriptor, PasswordWrapper: f.account.PasswordWrapper, RecoveryWrapper: f.account.RecoveryWrapper, Credential: f.credential[:]}, 201)
	var s Session
	if json.Unmarshal(b, &s) != nil || s.Token == "" {
		t.Fatal("session response")
	}
	return s
}
func TestServerIntegration(t *testing.T) {
	root := t.TempDir()
	var logs bytes.Buffer
	open := func() (*Server, *httptest.Server) {
		s, e := Open(Config{DataDir: root, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), RateBurst: 1000})
		if e != nil {
			t.Fatal(e)
		}
		return s, httptest.NewTLSServer(s)
	}
	s, ts := open()
	c := ts.Client()
	f := newFixture(t)
	a := hexID(f.account.ID)
	session := enroll(t, c, ts.URL, f)
	request(t, c, ts.URL, "POST", "/v1/signup", "", Signup{Account: a, Descriptor: f.account.Descriptor, PasswordWrapper: f.account.PasswordWrapper, RecoveryWrapper: f.account.RecoveryWrapper, Credential: f.credential[:]}, 409)
	request(t, c, ts.URL, "GET", "/v1/accounts/"+a, "", nil, 200)
	verifier := vc.Verifier(f.credential)
	request(t, c, ts.URL, "POST", "/v1/login", "", Login{Account: a, Credential: verifier[:]}, 401)
	b := request(t, c, ts.URL, "POST", "/v1/login", "", Login{Account: a, Credential: f.credential[:]}, 200)
	var second Session
	json.Unmarshal(b, &second)
	request(t, c, ts.URL, "GET", "/v1/devices", session.Token, nil, 200)
	request(t, c, ts.URL, "DELETE", "/v1/devices/"+second.Device, session.Token, nil, 204)
	request(t, c, ts.URL, "GET", "/v1/devices", second.Token, nil, 401)
	request(t, c, ts.URL, "GET", "/v1/devices", "", nil, 401)
	other := enroll(t, c, ts.URL, newFixture(t))
	request(t, c, ts.URL, "DELETE", "/v1/devices/"+session.Device, other.Token, nil, 404)
	fid, _ := vc.RandomID()
	mid, _ := vc.RandomID()
	fk, _ := vc.RandomKey()
	plaintext := []byte("synthetic confidential document M2 needle 48219")
	chunk, e := vc.Seal(fk, vc.Context{Kind: vc.Chunk, Account: f.account.ID, Object: fid, Final: true}, plaintext, nil)
	if e != nil {
		t.Fatal(e)
	}
	path := "/v1/blobs/" + hexID(fid) + "/chunks/0"
	request(t, c, ts.URL, "PUT", path, session.Token, chunk[:len(chunk)-1], 400)
	request(t, c, ts.URL, "GET", path, session.Token, nil, 404)
	request(t, c, ts.URL, "PUT", path, session.Token, chunk, 201)
	request(t, c, ts.URL, "PUT", path, session.Token, chunk, 204)
	changed := bytes.Clone(chunk)
	changed[len(changed)-1] ^= 1
	request(t, c, ts.URL, "PUT", path, session.Token, changed, 409)
	request(t, c, ts.URL, "GET", path, other.Token, nil, 404)
	request(t, c, ts.URL, "PUT", path, other.Token, chunk, 400)
	request(t, c, ts.URL, "GET", "/v1/blobs/"+hexID(fid)+"/chunks", session.Token, nil, 200)
	metadata := []byte(`{"type":"file","name":"private-M2-filename.pdf","parent":null,"tags":["private-M2-tag"],"favorite":false,"file_id":"` + hexID(fid) + `","size":47,"chunk_count":1}`)
	enc, _ := vc.Seal(f.master, vc.Context{Kind: vc.MetadataRecord, Account: f.account.ID, Object: mid}, metadata, nil)
	wrap, _ := vc.Seal(f.master, vc.Context{Kind: vc.FileKey, Account: f.account.ID, Object: fid}, fk[:], nil)
	record := Record{Metadata: enc, FileKey: wrap}
	mp := "/v1/metadata/" + hexID(mid)
	request(t, c, ts.URL, "PUT", mp, session.Token, record, 201)
	request(t, c, ts.URL, "PUT", mp, session.Token, record, 204)
	altered := bytes.Clone(enc)
	altered[len(altered)-1] ^= 1
	request(t, c, ts.URL, "PUT", mp, session.Token, Record{Metadata: altered, FileKey: wrap}, 409)
	request(t, c, ts.URL, "PUT", mp, session.Token, Record{Metadata: metadata, FileKey: wrap}, 400)
	request(t, c, ts.URL, "GET", mp, other.Token, nil, 404)
	request(t, c, ts.URL, "GET", "/v1/metadata", session.Token, nil, 200)
	ts.Close()
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	s, ts = open()
	defer s.Close()
	defer ts.Close()
	c = ts.Client()
	got := request(t, c, ts.URL, "GET", path, session.Token, nil, 200)
	if !bytes.Equal(got, chunk) {
		t.Fatal("restart lost chunk")
	}
	out, e := vc.Open(fk, vc.Context{Kind: vc.Chunk, Account: f.account.ID, Object: fid, Final: true}, got, nil)
	if e != nil || !bytes.Equal(out, plaintext) {
		t.Fatal("download decryption")
	}
	request(t, c, ts.URL, "GET", mp, session.Token, nil, 200)
	request(t, c, ts.URL, "GET", "/v1/devices", second.Token, nil, 401)
	needles := [][]byte{plaintext, []byte("private-M2-filename.pdf"), []byte("private-M2-tag"), []byte("synthetic M2 password never persisted"), f.master[:], fk[:], f.credential[:], []byte(session.Token)}
	check := func(b []byte) {
		for _, n := range needles {
			if bytes.Contains(b, n) || bytes.Contains(b, []byte(hex.EncodeToString(n))) || bytes.Contains(b, []byte(base64.StdEncoding.EncodeToString(n))) {
				t.Fatal("sensitive fixture persisted or logged")
			}
		}
	}
	if e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			check(b)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	check(logs.Bytes())
}
func TestLimitsAndExpiry(t *testing.T) {
	now := time.Now()
	s, e := Open(Config{DataDir: t.TempDir(), RateBurst: 100, SessionTTL: time.Second, Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ts := httptest.NewTLSServer(s)
	defer ts.Close()
	c := ts.Client()
	session := enroll(t, c, ts.URL, newFixture(t))
	request(t, c, ts.URL, "PUT", "/v1/blobs/11111111111111111111111111111111/chunks/0", session.Token, bytes.Repeat([]byte("x"), vc.ChunkSize+88), 413)
	now = now.Add(2 * time.Second)
	request(t, c, ts.URL, "GET", "/v1/devices", session.Token, nil, 401)
	request(t, c, ts.URL, "POST", "/v1/login", "", bytes.Repeat([]byte("x"), 8193), 413)
	request(t, c, ts.URL, "POST", "/v1/login", "", []byte(`{"account":"bad","credential":null,"extra":true}`), 400)
	plain := httptest.NewServer(s)
	defer plain.Close()
	request(t, plain.Client(), plain.URL, "GET", "/v1/devices", "", nil, 426)
}
func TestRateLimit(t *testing.T) {
	s, e := Open(Config{DataDir: t.TempDir(), RateBurst: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ts := httptest.NewTLSServer(s)
	defer ts.Close()
	request(t, ts.Client(), ts.URL, "POST", "/v1/login", "", Login{}, 400)
	request(t, ts.Client(), ts.URL, "POST", "/v1/login", "", Login{}, 429)
}

func TestResumableMultiChunk(t *testing.T) {
	root := t.TempDir()
	s, e := Open(Config{DataDir: root, RateBurst: 1000})
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewTLSServer(s)
	f := newFixture(t)
	session := enroll(t, ts.Client(), ts.URL, f)
	fid, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	prefix := "/v1/blobs/" + hexID(fid) + "/chunks"
	p := bytes.Repeat([]byte("M2 synthetic chunk body "), 200000)
	p = p[:vc.ChunkSize+123]
	first, e := vc.Seal(key, vc.Context{Kind: vc.Chunk, Account: f.account.ID, Object: fid}, p[:vc.ChunkSize], nil)
	if e != nil {
		t.Fatal(e)
	}
	last, e := vc.Seal(key, vc.Context{Kind: vc.Chunk, Account: f.account.ID, Object: fid, Index: 1, Final: true}, p[vc.ChunkSize:], nil)
	if e != nil {
		t.Fatal(e)
	}
	request(t, ts.Client(), ts.URL, "PUT", prefix+"/0", session.Token, first, 201)
	// Simulate a transport read failure before a complete second chunk arrives.
	req := httptest.NewRequest("PUT", prefix+"/1", nil)
	req.TLS = &tls.ConnectionState{}
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Body = &brokenBody{data: last[:90]}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatal("interrupted body accepted")
	}
	ts.Close()
	s.Close()
	s, e = Open(Config{DataDir: root, RateBurst: 1000})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ts = httptest.NewTLSServer(s)
	defer ts.Close()
	var indices []uint64
	b := request(t, ts.Client(), ts.URL, "GET", prefix, session.Token, nil, 200)
	if json.Unmarshal(b, &indices) != nil || len(indices) != 1 || indices[0] != 0 {
		t.Fatal("resume inventory")
	}
	request(t, ts.Client(), ts.URL, "PUT", prefix+"/1", session.Token, last, 201)
	request(t, ts.Client(), ts.URL, "PUT", prefix+"/0", session.Token, first, 204)
	var encrypted, plain bytes.Buffer
	encrypted.Write(request(t, ts.Client(), ts.URL, "GET", prefix+"/0", session.Token, nil, 200))
	encrypted.Write(request(t, ts.Client(), ts.URL, "GET", prefix+"/1", session.Token, nil, 200))
	if e := vc.DecryptStream(key, f.account.ID, fid, uint64(len(p)), &encrypted, &plain); e != nil || !bytes.Equal(p, plain.Bytes()) {
		t.Fatal("resumed complete file failed authentication")
	}
}

type brokenBody struct{ data []byte }

func (b *brokenBody) Read(p []byte) (int, error) {
	if len(b.data) > 0 {
		n := copy(p, b.data)
		b.data = b.data[n:]
		return n, nil
	}
	return 0, io.ErrUnexpectedEOF
}
func (b *brokenBody) Close() error { return nil }
func TestConcurrentChunkPublication(t *testing.T) {
	store, e := NewDiskStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a, _ := vc.RandomID()
	id, _ := vc.RandomID()
	var wg sync.WaitGroup
	results := make(chan bool, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, e := store.Put(hexID(a), hexID(id), 0, []byte("synthetic ciphertext"))
			results <- created
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	count := 0
	for b := range results {
		if b {
			count++
		}
	}
	for e := range errs {
		if e != nil {
			t.Fatal("concurrent publication failed")
		}
	}
	if count != 1 {
		t.Fatal("chunk published more than once")
	}
}
