package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	vc "klefki/core/crypto"
	"klefki/core/local"
	vs "klefki/core/sync"
)

type request struct {
	Op, Password, Path, Name string
	History, DelayMS         int
}

type trace struct {
	Method    string  `json:"method"`
	Path      string  `json:"path"`
	StartMS   float64 `json:"start_ms"`
	ElapsedMS float64 `json:"elapsed_ms"`
	Status    int     `json:"status"`
}

type metrics struct {
	started       time.Time
	Requests      int      `json:"requests"`
	RequestBytes  int64    `json:"request_bytes"`
	ResponseBytes int64    `json:"response_bytes"`
	ChunksUp      int      `json:"chunks_up"`
	ChunksDown    int      `json:"chunks_down"`
	HTTPErrors    []int    `json:"http_errors"`
	Trace         []*trace `json:"trace"`
}

type transport struct {
	metrics *metrics
	delay   time.Duration
}

type body struct {
	io.ReadCloser
	metrics *metrics
	trace   *trace
	started time.Time
}

func (b *body) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.metrics.ResponseBytes += int64(n)
	return n, err
}

func (b *body) Close() error {
	err := b.ReadCloser.Close()
	b.trace.ElapsedMS = float64(time.Since(b.started).Microseconds()) / 1000
	return err
}

func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	x := &trace{Method: r.Method, Path: r.URL.Path, StartMS: float64(time.Since(t.metrics.started).Microseconds()) / 1000}
	t.metrics.Trace = append(t.metrics.Trace, x)
	if t.delay > 0 {
		timer := time.NewTimer(t.delay)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-timer.C:
		}
	}
	t.metrics.Requests++
	if r.ContentLength > 0 {
		t.metrics.RequestBytes += r.ContentLength
	}
	if strings.Contains(r.URL.Path, "/chunks/") {
		if r.Method == "PUT" {
			t.metrics.ChunksUp++
		} else if r.Method == "GET" {
			t.metrics.ChunksDown++
		}
	}
	response, err := http.DefaultTransport.RoundTrip(r)
	if err == nil {
		x.Status = response.StatusCode
		response.Body = &body{response.Body, t.metrics, x, start}
		if response.StatusCode >= 400 {
			t.metrics.HTTPErrors = append(t.metrics.HTTPErrors, response.StatusCode)
		}
	}
	return response, err
}

func main() {
	accountPath := flag.String("account", "", "benchmark account file")
	storePath := flag.String("store", "", "fresh benchmark store")
	address := flag.String("server", "", "relay origin")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var store *vs.Store
	var master vc.Key
	defer func() {
		if store != nil {
			store.Close()
		}
		vc.Wipe(master[:])
	}()
	m := &metrics{}
	t := &transport{metrics: m}
	var client *vs.Client
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var command request
		err := json.Unmarshal(scanner.Bytes(), &command)
		start := time.Now()
		*m = metrics{started: start, Trace: []*trace{}, HTTPErrors: []int{}}
		t.delay = time.Duration(command.DelayMS) * time.Millisecond
		out := map[string]any{}
		callCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
		if err == nil && (command.DelayMS < 0 || command.DelayMS > 1000) {
			err = errors.New("invalid injected delay")
		}
		if err == nil && command.Op != "init" && client == nil {
			err = errors.New("client not initialized")
		}
		if err == nil {
			switch command.Op {
			case "init":
				if client != nil {
					err = errors.New("client already initialized")
					break
				}
				var account vc.Account
				account, err = local.LoadAccount(*accountPath)
				if err == nil {
					master, err = account.Unlock([]byte(command.Password))
				}
				if err == nil {
					store, err = vs.Open(*storePath, account.ID)
				}
				var remote *vs.Remote
				if err == nil {
					remote, err = vs.NewRemote(*address, &http.Client{Transport: t, Timeout: 30 * time.Second}, true)
				}
				if err == nil {
					keys, e := vc.Derive([]byte(command.Password), account.Descriptor)
					err = e
					if err == nil {
						credential := vc.Credential(keys.Auth, account.ID)
						keys.Clear()
						session, e := remote.Login(callCtx, account.ID, credential)
						err = e
						client = &vs.Client{Store: store, Remote: remote, Session: session, Master: master}
						out["session_device"] = session.Device
					}
				}
				if err == nil {
					state, e := store.RelayState()
					err = e
					out["device"] = state.Device
				}
			case "seed":
				if command.History < 0 || command.History > 4096 {
					err = errors.New("history must be between 0 and 4096")
					break
				}
				for i := 0; i < command.History && err == nil; i++ {
					id, e := vc.RandomID()
					err = e
					if err == nil {
						_, err = store.Put(master, id.String(), "", vc.Metadata{Type: "folder", Name: fmt.Sprintf("benchmark-history-%04d", i), Tags: []string{}}, nil)
					}
				}
			case "put":
				var f *os.File
				f, err = os.Open(command.Path)
				if err == nil {
					info, e := f.Stat()
					err = e
					if err == nil {
						id, e := vc.RandomID()
						err = e
						if err == nil {
							out["revision"], err = store.Put(master, id.String(), "", vc.Metadata{Type: "file", Name: command.Name, Tags: []string{}, Size: uint64(info.Size())}, f)
						}
					}
					f.Close()
				}
			case "sync":
				err = client.Sync(callCtx)
				if errors.Is(err, vs.ErrRelayWaiting) {
					out["waiting"] = true
					err = nil
				}
				if err == nil {
					out["settings"], err = client.Settings(callCtx)
				}
			case "verify":
				matches := []map[string]any{}
				heads, e := store.Heads(master)
				err = e
				for _, h := range heads {
					if h.Metadata.Name == command.Name && err == nil {
						hash := sha256.New()
						err = store.Export(master, h.Revision, hash)
						matches = append(matches, map[string]any{"revision": h.Revision, "sha256": hex.EncodeToString(hash.Sum(nil)), "size": h.Metadata.Size})
					}
				}
				out["matches"] = matches
			case "summary":
				heads, e := store.Heads(master)
				err = e
				files := 0
				for _, h := range heads {
					if h.Metadata.Type == "file" {
						files++
					}
				}
				out["file_heads"] = files
				out["revision_heads"] = len(heads)
			default:
				err = errors.New("unknown benchmark command")
			}
		}
		cancel()
		out["seconds"] = time.Since(start).Seconds()
		out["metrics"] = m
		out["injected_delay_ms"] = command.DelayMS
		if err != nil {
			out["error"] = err.Error()
		}
		json.NewEncoder(os.Stdout).Encode(out)
	}
}
