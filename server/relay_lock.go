package server

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"

	vc "klefki/core/crypto"
	"klefki/core/syncwire"
)

type accountLock struct {
	mu   sync.Mutex
	refs int
}
type accountLocks struct {
	mu      sync.Mutex
	entries map[string]*accountLock
}

func (l *accountLocks) lock(account string) func() {
	l.mu.Lock()
	if l.entries == nil {
		l.entries = make(map[string]*accountLock)
	}
	entry := l.entries[account]
	if entry == nil {
		entry = &accountLock{}
		l.entries[account] = entry
	}
	entry.refs++
	l.mu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.mu.Lock()
		defer l.mu.Unlock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.entries, account)
		}
	}
}

// All relay handlers have bounded responses. Buffer while holding the account
// lock so slow readers cannot block publication, cleanup, or another account.
type relayResponse struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	limit    int
	overflow bool
}

func (w *relayResponse) Header() http.Header { return w.header }
func (w *relayResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *relayResponse) Write(b []byte) (int, error) {
	if w.body.Len()+len(b) > w.limit {
		w.overflow = true
		return 0, io.ErrShortBuffer
	}
	w.WriteHeader(http.StatusOK)
	return w.body.Write(b)
}
func (s *Server) relayBuffered(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
			limit := int64(16384)
			if strings.HasPrefix(r.URL.Path, "/v1/blobs/") {
				limit = vc.ChunkSize + 87
			}
			if strings.HasPrefix(r.URL.Path, "/v1/metadata/") || strings.HasPrefix(r.URL.Path, "/v1/sync/revisions/") {
				limit = 1500000
			}
			b, ok := readBody(w, r, limit)
			if !ok {
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		limit := syncwire.MaxPageBytes
		if r.URL.Path == "/v1/sync/relay/prepare" || r.URL.Path == "/v1/sync/relay/need-batch" {
			limit = 16384
		}
		response := &relayResponse{header: make(http.Header), limit: limit}
		func() { unlock := s.relayLocks.lock(who(r).account); defer unlock(); next.ServeHTTP(response, r) }()
		if response.overflow {
			fail(w, 500)
			return
		}
		for k, v := range response.header {
			w.Header()[k] = v
		}
		if response.status == 0 {
			response.status = http.StatusOK
		}
		w.WriteHeader(response.status)
		_, _ = w.Write(response.body.Bytes())
	})
}
