package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	vc "vault/core/crypto"
	"vault/core/local"
	vs "vault/core/sync"
)

const backupHelp = `vault backup [--vault PATH] [--store PATH] [--password-file PATH | --recovery-file PATH] -o DIRECTORY
Backup/restore destinations must not exist. Close desktop before using its store.
`

func runBackup(args []string, out io.Writer) error {
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	accountPath := flags.String("vault", filepath.Join(".vault", "account.v1"), "")
	storePath := flags.String("store", "", "")
	passwordPath := flags.String("password-file", "", "")
	recoveryPath := flags.String("recovery-file", "", "")
	dest := flags.String("o", "", "")
	if flags.Parse(args[1:]) != nil || (*passwordPath != "" && *recoveryPath != "") {
		return errUsage
	}
	if flags.NArg() != 0 {
		return errUsage
	}
	if (command == "check" && *dest != "") || (command != "check" && *dest == "") {
		return errUsage
	}
	a, e := local.LoadAccount(*accountPath)
	if e != nil {
		return e
	}
	var master vc.Key
	if *recoveryPath != "" {
		secret, e := secretFile(*recoveryPath, 57)
		if e != nil {
			return e
		}
		master, e = a.Recover(string(secret))
		vc.Wipe(secret)
		if e != nil {
			return e
		}
	} else {
		secret, e := password(*passwordPath, false)
		if e != nil {
			return e
		}
		master, e = a.Unlock(secret)
		vc.Wipe(secret)
		if e != nil {
			return e
		}
	}
	defer vc.Wipe(master[:])
	if *storePath == "" {
		*storePath = filepath.Join(filepath.Dir(*accountPath), "local.db")
	}
	// Open normally creates a store; backup must never mistake a missing store for an empty vault.
	stat, e := os.Stat(*storePath)
	if e != nil || !stat.Mode().IsRegular() || stat.Size() == 0 {
		return vc.ErrInvalid
	}
	source, e := vs.OpenExisting(*storePath, a.ID)
	if e != nil {
		return e
	}
	defer source.Close()
	if e = os.Mkdir(*dest, 0700); e != nil {
		if os.IsExist(e) {
			return errExists
		}
		return vc.ErrIO
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(*dest)
		}
	}()
	snapshotPath := filepath.Join(*dest, "local.db")

	e = source.Snapshot(snapshotPath)
	if e != nil {
		return e
	}
	snapshot, e := vs.OpenExisting(snapshotPath, a.ID)
	if e != nil {
		return e
	}
	defer snapshot.Close()
	revisions, e := snapshot.Check(master)
	if e != nil {
		return e
	}

	e = local.Publish(filepath.Join(*dest, "account.v1"), func(w io.Writer) error { return writeAll(w, local.AccountBytes(a)) })

	if e != nil {
		return e
	}
	if e = snapshot.Close(); e != nil {
		return e
	}
	complete = true
	_, e = fmt.Fprintf(out, "%s complete: %d revisions verified.\n", command, len(revisions))
	return e
}
