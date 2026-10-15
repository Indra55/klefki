package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	vc "klefki/core/crypto"
	vs "klefki/core/sync"
	"klefki/core/syncwire"
)

const syncHelp = `klefki register [--klefki PATH] [--password-file PATH] --server URL [--dev-http]
klefki add [--klefki PATH] [--password-file PATH] [--store PATH] [--object ID --base REVISION] FILE
klefki list [--klefki PATH] [--password-file PATH] [--store PATH]
klefki export [--klefki PATH] [--password-file PATH] [--store PATH] -o PATH REVISION
klefki sync [--klefki PATH] [--password-file PATH] [--store PATH] --server URL [--sync-mode persistent|temporary-relay] [--forget-device ID] [--trash-sync follow-vault|on|off] [--vault-trash-sync on|off] [--dev-http]
Sync flags must precede positional arguments. HTTP requires explicit loopback development mode.`

func runSync(args []string, out io.Writer) error {
	command := args[0]
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	accountPath := f.String("klefki", filepath.Join(".klefki", "account.v1"), "")
	passwordPath := f.String("password-file", "", "")
	var storePath, serverURL, object, base, dest string
	var dev bool
	trashOverride, trashDefault := "", ""
	mode := ""
	forget := ""
	if command != "register" {
		f.StringVar(&storePath, "store", "", "")
	}
	if command == "register" || command == "sync" {
		f.StringVar(&serverURL, "server", "", "")
		f.BoolVar(&dev, "dev-http", false, "")
	}
	if command == "sync" {
		f.StringVar(&mode, "sync-mode", "", "")
		f.StringVar(&trashOverride, "trash-sync", "", "")
		f.StringVar(&trashDefault, "vault-trash-sync", "", "")
		f.StringVar(&forget, "forget-device", "", "")
	}
	if command == "add" {
		f.StringVar(&object, "object", "", "")
		f.StringVar(&base, "base", "", "")
	}
	if command == "export" {
		f.StringVar(&dest, "o", "", "")
	}
	if f.Parse(args[1:]) != nil {
		return errUsage
	}
	if trashOverride != "" && trashOverride != "follow-vault" && trashOverride != "on" && trashOverride != "off" {
		return errUsage
	}
	if trashDefault != "" && trashDefault != "on" && trashDefault != "off" {
		return errUsage
	}
	if mode != "" && mode != syncwire.Persistent && mode != syncwire.TemporaryRelay {
		return errUsage
	}
	if forget != "" {
		if _, e := vc.ParseID(forget); e != nil {
			return errUsage
		}
	}
	if command == "add" || command == "export" {
		if f.NArg() != 1 {
			return errUsage
		}
	} else if f.NArg() != 0 {
		return errUsage
	}
	if (command == "sync" || command == "register") && serverURL == "" {
		return errUsage
	}
	if command == "export" && dest == "" {
		return errUsage
	}
	if command == "add" && (object == "") != (base == "") {
		return errUsage
	}
	account, e := loadAccount(*accountPath)
	if e != nil {
		return e
	}
	password, e := password(*passwordPath, false)
	if e != nil {
		return e
	}
	defer vc.Wipe(password)
	master, e := account.Unlock(password)
	if e != nil {
		return e
	}
	defer vc.Wipe(master[:])
	if command == "register" {
		return networkSync(nil, serverURL, dev, account, master, password, true, "", "", out)
	}
	if storePath == "" {
		storePath = filepath.Join(filepath.Dir(*accountPath), "local.db")
	}
	store, e := vs.Open(storePath, account.ID)
	if e != nil {
		return e
	}
	defer store.Close()
	switch command {
	case "add":
		file, e := os.Open(f.Arg(0))
		if e != nil {
			return vc.ErrIO
		}
		defer file.Close()
		stat, e := file.Stat()
		if e != nil || !stat.Mode().IsRegular() || stat.Size() < 0 || uint64(stat.Size()) > vc.MaxFileSize {
			return vc.ErrInvalid
		}
		if object == "" {
			id, e := vc.RandomID()
			if e != nil {
				return e
			}
			object = id.String()
		}
		revision, e := store.Put(master, object, base, vc.Metadata{Type: "file", Name: filepath.Base(f.Arg(0)), Tags: []string{}, Size: uint64(stat.Size())}, file)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(struct {
			Object   string `json:"object"`
			Revision string `json:"revision"`
		}{object, revision})
	case "list":
		heads, e := store.Heads(master)
		if e != nil {
			return e
		}
		type row struct {
			Object   string `json:"object"`
			Revision string `json:"revision"`
			Conflict bool   `json:"conflict"`
		}
		rows := []row{}
		for _, h := range heads {
			rows = append(rows, row{h.Object, h.Revision, h.Conflict})
		}
		return json.NewEncoder(out).Encode(rows)
	case "export":
		if _, e := vc.ParseID(f.Arg(0)); e != nil {
			return e
		}
		if e := publish(dest, func(w io.Writer) error { return store.Export(master, f.Arg(0), w) }); e != nil {
			return e
		}
		_, e = fmt.Fprintln(out, "Revision exported.")
		return e
	case "sync":
		return networkSync(store, serverURL, dev, account, master, password, false, mode, forget, out, trashOverride, trashDefault)
	}
	return errUsage
}
func networkSync(store *vs.Store, address string, dev bool, account vc.Account, master vc.Key, password []byte, register bool, mode, forget string, out io.Writer, trashOptions ...string) error {
	defer vc.Wipe(master[:])
	remote, e := vs.NewRemote(address, nil, dev)
	if e != nil {
		return e
	}
	keys, e := vc.Derive(password, account.Descriptor)
	if e != nil {
		return e
	}
	defer keys.Clear()
	credential := vc.Credential(keys.Auth, account.ID)
	defer vc.Wipe(credential[:])
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var session vs.Session
	if register {
		session, e = remote.Signup(ctx, account, credential)
	} else {
		local, err := store.RelayState()
		if err != nil {
			return err
		}
		session, e = remote.LoginAccount(ctx, account, credential, local.Mode == syncwire.TemporaryRelay)
	}
	if e != nil {
		return e
	}
	// CLI sessions are ephemeral. Persistent device sessions remain available to
	// core callers; no bearer token is written to the local store or stdout.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = remote.Revoke(cleanup, session)
	}()
	if register {
		_, e = fmt.Fprintln(out, "Account registered.")
		return e
	}
	client := vs.Client{Store: store, Remote: remote, Session: session, Master: master}
	defer vc.Wipe(client.Master[:])
	if forget != "" {
		if e = client.ForgetDevice(ctx, forget); e != nil {
			return e
		}
	}
	if mode != "" {
		if e = client.SetMode(ctx, mode); e != nil {
			return e
		}
	}
	if len(trashOptions) > 0 && trashOptions[0] != "" {
		if e = store.SetTrashOverride(trashOptions[0]); e != nil {
			return e
		}
	}
	if len(trashOptions) > 1 && trashOptions[1] != "" {
		if e = client.SetTrashDefault(ctx, trashOptions[1] == "on"); e != nil {
			return e
		}
	}
	e = client.SyncAccount(ctx, account, credential)
	session = client.Session
	if e != nil {
		return e
	}
	_, e = fmt.Fprintln(out, "Sync complete.")
	return e
}
