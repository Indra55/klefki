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

	vc "klefki/core/crypto"
	"klefki/core/search"
	"klefki/core/semantic"
	vs "klefki/core/sync"
)

const searchHelp = `klefki index [--klefki PATH] [--store PATH] [--password-file PATH | --recovery-file PATH] [--language eng]
klefki model setup|status [--model-dir PATH] [--bundle DIR]
klefki analyze|correct --revision ID [--correction-file PATH] [--cutoff N] [--margin N]
klefki evaluate --labels-file PATH [--model-dir PATH]
klefki index --semantic [--model-dir PATH]
klefki search --mode keyword|hybrid [--model-dir PATH]
klefki search [--klefki PATH] [--store PATH] [--password-file PATH | --recovery-file PATH] [--query-file PATH]
Search reads the query from standard input when --query-file is omitted.
Indexing uses local Tesseract/Poppler; run klefki sync afterward to sync the encrypted index.`

func runSearch(args []string, out io.Writer) error {
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	accountPath := f.String("klefki", filepath.Join(".klefki", "account.v1"), "")
	storePath := f.String("store", "", "")
	passwordPath := f.String("password-file", "", "")
	recoveryPath := f.String("recovery-file", "", "")
	language, queryPath := "eng", ""
	semanticIndex := f.Bool("semantic", false, "")
	mode := f.String("mode", "keyword", "")
	modelDir := f.String("model-dir", filepath.Join(".klefki", "models", semantic.Fingerprint()), "")
	revision := f.String("revision", "", "")
	labelsPath := f.String("labels-file", "", "")
	correctionPath := f.String("correction-file", "", "")
	cutoff := f.Float64("cutoff", semantic.DefaultThresholds.Cutoff, "")
	margin := f.Float64("margin", semantic.DefaultThresholds.Margin, "")
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
	var model semantic.Embedder
	if *semanticIndex || *mode == "hybrid" || args[0] == "evaluate" {
		model, e = semantic.Open(*modelDir)
		if e != nil {
			return e
		}
		defer model.Close()
	}
	if args[0] == "analyze" {
		a, e := search.Analyze(ctx, s, key, *revision, semantic.Thresholds{Cutoff: *cutoff, Margin: *margin})
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(a)
	}
	if args[0] == "correct" {
		var input struct {
			Category string           `json:"category"`
			Fields   []semantic.Field `json:"fields"`
			Resolve  []string         `json:"resolve"`
		}
		if e := readSearchJSON(*correctionPath, &input); e != nil {
			return e
		}
		id, e := search.SaveCorrection(ctx, s, key, *revision, input.Category, input.Fields, input.Resolve)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(struct {
			Revision string `json:"revision"`
		}{id})
	}
	if args[0] == "evaluate" {
		var labels search.EvaluationSet
		if e := readSearchJSON(*labelsPath, &labels); e != nil {
			return e
		}
		result, e := search.Evaluate(ctx, s, key, model, labels)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(result)
	}
	if *mode != "keyword" && *mode != "hybrid" {
		return errUsage
	}
	if args[0] == "index" {
		var report search.Report
		if *semanticIndex {
			report, e = search.BuildSemantic(ctx, s, key, search.Extractor{Language: language}, model)
		} else {
			report, e = search.Build(ctx, s, key, search.Extractor{Language: language})
		}
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
	hits, e := search.QueryMode(ctx, s, key, model, string(query), *mode)
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(hits)
}

func readSearchJSON(path string, out any) error {
	f, e := os.Open(path)
	if e != nil {
		return vc.ErrIO
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8<<20))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return errUsage
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errUsage
	}
	return nil
}

func runModel(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	f := flag.NewFlagSet("model", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	root := f.String("model-dir", filepath.Join(".klefki", "models", semantic.Fingerprint()), "")
	bundle := f.String("bundle", "", "")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 {
		return errUsage
	}
	if args[0] == "status" {
		return json.NewEncoder(out).Encode(struct {
			Supported   bool   `json:"supported"`
			Ready       bool   `json:"ready"`
			Bytes       int64  `json:"downloadBytes"`
			Fingerprint string `json:"fingerprint"`
		}{semantic.Supported(), semantic.Verify(*root) == nil, semantic.DownloadSize(), semantic.Fingerprint()})
	}
	if args[0] != "setup" {
		return errUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := semantic.Install(ctx, *root, *bundle, func(done, total int64) { fmt.Fprintf(os.Stderr, "\rModel assets: %d / %d bytes", done, total) }); e != nil {
		return e
	}
	fmt.Fprintln(os.Stderr)
	_, e := fmt.Fprintln(out, "Local model ready. Use index --semantic and search --mode hybrid to opt in.")
	return e
}
