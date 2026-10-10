// Synthetic cross-client fixtures. Uses the same crypto/store/sync packages as desktop.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	vc "klefki/core/crypto"
	vd "klefki/core/desktop"
	"klefki/core/local"
	"klefki/core/search"
	"klefki/core/semantic"
	vs "klefki/core/sync"
)

const password = "public M5 browser fixture password"

func must(e error) {
	if e != nil {
		panic("fixture operation failed")
	}
}
func payload() []byte {
	b := make([]byte, vc.ChunkSize+19)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}
func main() {
	if len(os.Args) != 4 {
		panic("fixture arguments")
	}
	mode, dir, address := os.Args[1], os.Args[2], os.Args[3]
	must(os.MkdirAll(dir, 0700))
	var a vc.Account
	var master vc.Key
	var e error
	if mode == "seed" {
		var recovery vc.Key
		a, master, recovery, e = vc.NewAccount([]byte(password))
		must(e)
		vc.Wipe(recovery[:])
		must(os.WriteFile(filepath.Join(dir, "account.klefki"), local.AccountBytes(a), 0600))
	} else {
		a, e = local.LoadAccount(filepath.Join(dir, "account.klefki"))
		must(e)
		master, e = a.Unlock([]byte(password))
		must(e)
	}
	defer vc.Wipe(master[:])
	keys, e := vc.Derive([]byte(password), a.Descriptor)
	must(e)
	credential := vc.Credential(keys.Auth, a.ID)
	keys.Clear()
	defer vc.Wipe(credential[:])
	store, e := vs.Open(filepath.Join(dir, "desktop.db"), a.ID)
	must(e)
	defer store.Close()
	remote, e := vs.NewRemote(address, nil, true)
	must(e)
	ctx := context.Background()
	var session vs.Session
	if mode == "seed" {
		session, e = remote.Signup(ctx, a, credential)
	} else {
		session, e = remote.Login(ctx, a.ID, credential)
	}
	must(e)
	defer remote.Revoke(ctx, session)
	client := vs.Client{Store: store, Remote: remote, Session: session, Master: master}
	defer vc.Wipe(client.Master[:])
	switch mode {
	case "relevance":
		for _, d := range []struct{ name, text string }{
			{"utility.txt", "Electricity statement: consumption history visualization, outstanding balance INR 1500. Payment instructions: settle your bill online. Account 731924."},
			{"resume.txt", "A software engineer with employment history and experience developing applications for clients."},
			{"match.txt", "A football match with a score of 2 to 1. Highlights for fans."},
			{"unrelated.txt", "Instructions for making a chart with a visualization of tournament results."},
		} {
			id, e := vc.RandomID()
			must(e)
			_, e = store.Put(master, id.String(), "", vc.Metadata{Type: "file", Name: d.name, Tags: []string{}, Size: uint64(len(d.text))}, bytes.NewBufferString(d.text))
			must(e)
		}
		model, e := semantic.Open(os.Getenv("KLEFKI_MODEL_DIR"))
		must(e)
		defer model.Close()
		_, e = search.BuildSemantic(ctx, store, master, search.Extractor{}, model)
		must(e)
		must(client.Sync(ctx))
		fmt.Println("PASS relevance fixture")
	case "index-semantic":
		model, e := semantic.Open(os.Getenv("KLEFKI_MODEL_DIR"))
		must(e)
		defer model.Close()
		_, e = search.BuildSemantic(ctx, store, master, search.Extractor{}, model)
		must(e)
		must(client.Sync(ctx))
		fmt.Println("PASS semantic index")
	case "verify-correction":
		must(client.Sync(ctx))
		heads, e := store.Heads(master)
		must(e)
		cs, e := search.Corrections(ctx, store, master, heads)
		must(e)
		if len(cs) != 1 || cs[0].Correction.Category != "Money" {
			panic("correction missing")
		}
		fmt.Println("PASS correction")
	case "seed":
		must(store.Close())
		finished := make(chan vd.Event, 8)
		desktop, e := vd.New(filepath.Join(dir, "desktop-settings"), func(event vd.Event) {
			if event.Kind == "finished" {
				finished <- event
			}
		})
		must(e)
		must(desktop.Select(filepath.Join(dir, "account.klefki"), filepath.Join(dir, "desktop.db"), false))
		must(desktop.Unlock(password, false))
		paths := []string{}
		for _, fixture := range []struct {
			name string
			data []byte
		}{{"desktop.bin", payload()}, {"note.txt", []byte("original desktop text")}, {"empty.txt", []byte{}}} {
			path := filepath.Join(dir, fixture.name)
			must(os.WriteFile(path, fixture.data, 0600))
			paths = append(paths, path)
		}
		_, e = desktop.Import(paths, nil, []string{})
		must(e)
		select {
		case event := <-finished:
			if event.Error != "" {
				panic("desktop import failed")
			}
		case <-time.After(20 * time.Second):
			panic("desktop import timeout")
		}
		desktop.Close()
		store, e = vs.Open(filepath.Join(dir, "desktop.db"), a.ID)
		must(e)
		defer store.Close()
		client.Store = store
		must(client.Sync(ctx))
	case "desktop-edit":
		heads, e := store.Heads(master)
		must(e)
		for _, h := range heads {
			if h.Metadata.Name == "note.txt" {
				data := []byte("desktop conflict text")
				_, e = store.Put(master, h.Object, h.Revision, vc.Metadata{Type: "file", Name: "note.txt", Tags: []string{}, Size: uint64(len(data))}, bytes.NewReader(data))
				must(e)
			}
		}
		must(client.Sync(ctx))
	case "verify":
		must(client.Sync(ctx))
		heads, e := store.Heads(master)
		must(e)
		seen := map[string]bool{}
		for _, h := range heads {
			var out bytes.Buffer
			if h.Metadata.Type != "file" {
				continue
			}
			must(store.Export(master, h.Revision, &out))
			switch h.Metadata.Name {
			case "desktop.bin":
				if !bytes.Equal(out.Bytes(), payload()) {
					panic("desktop payload mismatch")
				}
				seen["desktop"] = true
			case "web.txt":
				if out.String() != "uploaded from web" {
					panic("web payload mismatch")
				}
				seen["web"] = true
			case "note.txt":
				if out.String() == "offline web text" {
					seen["edit"] = true
				}
			}
		}
		must(store.Close())
		desktop, e := vd.New(filepath.Join(dir, "desktop-preview"), func(vd.Event) {})
		must(e)
		defer desktop.Close()
		must(desktop.Select(filepath.Join(dir, "account.klefki"), filepath.Join(dir, "desktop.db"), false))
		must(desktop.Unlock(password, false))
		for _, h := range heads {
			if h.Metadata.Name == "web.txt" {
				preview, e := desktop.Preview(h.Revision)
				must(e)
				if string(preview.Data) != "uploaded from web" {
					panic("desktop preview mismatch")
				}
				vc.Wipe(preview.Data)
			}
		}
		if !seen["desktop"] || !seen["web"] || !seen["edit"] {
			panic("cross-client files missing")
		}
	case "verify-conflict":
		must(client.Sync(ctx))
		heads, e := store.Heads(master)
		must(e)
		seen := map[string]bool{}
		for _, h := range heads {
			if h.Metadata.Name == "note.txt" && h.Conflict {
				var out bytes.Buffer
				must(store.Export(master, h.Revision, &out))
				seen[out.String()] = true
			}
		}
		if !seen["desktop conflict text"] || !seen["offline web text"] {
			panic("conflict copy missing")
		}
	default:
		panic("unknown fixture mode")
	}
	fmt.Println("PASS " + mode)
}
