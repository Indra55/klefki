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

	vc "vault/core/crypto"
	"vault/core/search"
	vs "vault/core/sync"
)

const searchHelp = `vault index [--vault PATH] [--store PATH] [--password-file PATH | --recovery-file PATH] [--language eng]
vault search [--vault PATH] [--store PATH] [--password-file PATH | --recovery-file PATH] [--query-file PATH]
Search reads the query from standard input when --query-file is omitted.
Indexing uses local Tesseract/Poppler; run vault sync afterward to sync the encrypted index.`

func runSearch(args []string, out io.Writer) error {
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	accountPath := f.String("vault", filepath.Join(".vault", "account.v1"), "")
	storePath := f.String("store", "", "")
	passwordPath := f.String("password-file", "", "")
	recoveryPath := f.String("recovery-file", "", "")
	language, queryPath := "eng", ""
	if args[0] == "index" {
		f.StringVar(&language, "language", "eng", "")
	} else {
		f.StringVar(&queryPath, "query-file", "", "")
	}
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || (*passwordPath != "" && *recoveryPath != "") {
		return errUsage
	}
	account, e := loadAccount(*accountPath)
	if e != nil {
		return e
	}
	var key vc.Key
	if *recoveryPath != "" {
		secret, e := secretFile(*recoveryPath, 57)
		if e != nil {
			return e
		}
		key, e = account.Recover(string(secret))
		vc.Wipe(secret)
		if e != nil {
			return e
		}
	} else {
		secret, e := password(*passwordPath, false)
		if e != nil {
			return e
		}
		key, e = account.Unlock(secret)
		vc.Wipe(secret)
		if e != nil {
			return e
		}
	}
	defer vc.Wipe(key[:])
	if *storePath == "" {
		*storePath = filepath.Join(filepath.Dir(*accountPath), "local.db")
	}
	s, e := vs.OpenExisting(*storePath, account.ID)
	if e != nil {
		return e
	}
	defer s.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if args[0] == "index" {
		report, e := search.Build(ctx, s, key, search.Extractor{Language: language})
		if e != nil {
			return e
		}
		_, e = fmt.Fprintf(out, "Index rebuilt: %d documents; %d indexed by name/tags only (unsupported content type).\n", report.Documents, report.MetadataOnly)
		return e
	}
	input := io.Reader(os.Stdin)
	if queryPath != "" {
		file, e := os.Open(queryPath)
		if e != nil {
			return vc.ErrIO
		}
		defer file.Close()
		input = file
	}
	query, e := io.ReadAll(io.LimitReader(input, 4097))
	if e != nil {
		return vc.ErrIO
	}
	defer vc.Wipe(query)
	if len(query) > 4096 {
		return search.ErrLimit
	}
	hits, e := search.Query(ctx, s, key, string(query))
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(hits)
}
