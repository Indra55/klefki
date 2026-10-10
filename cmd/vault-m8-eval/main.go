// Public synthetic M8 acceptance runner. Private labels use vault evaluate instead.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	vc "vault/core/crypto"
	"vault/core/search"
	"vault/core/semantic"
	vs "vault/core/sync"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "M8 evaluation failed:", e)
		os.Exit(1)
	}
}
func run() error {
	modelDir := flag.String("model-dir", "", "")
	fixture := flag.String("fixture", "core/search/testdata/m8-evaluation.json", "")
	output := flag.String("output", "", "")
	parity := flag.String("parity-output", "", "")
	flag.Parse()
	data, e := os.ReadFile(*fixture)
	if e != nil {
		return e
	}
	var corpus struct {
		Documents []struct {
			Name     string `json:"name"`
			Text     string `json:"text"`
			Category string `json:"category"`
		} `json:"documents"`
		Queries []struct {
			Query    string   `json:"query"`
			Expected []string `json:"expected"`
			Kind     string   `json:"kind"`
		} `json:"queries"`
	}
	if json.Unmarshal(data, &corpus) != nil {
		return search.ErrIndex
	}
	model, e := semantic.Open(*modelDir)
	if e != nil {
		return e
	}
	defer model.Close()
	root, e := os.MkdirTemp("", "vault-m8-eval-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	id, _ := vc.RandomID()
	key, _ := vc.RandomKey()
	defer vc.Wipe(key[:])
	s, e := vs.Open(filepath.Join(root, "local.db"), id)
	if e != nil {
		return e
	}
	defer s.Close()
	ctx := context.Background()
	labels := search.EvaluationSet{}
	byName := map[string]string{}
	for _, d := range corpus.Documents {
		object, _ := vc.RandomID()
		rev, e := s.Put(key, object.String(), "", vc.Metadata{Type: "file", Name: d.Name, Tags: []string{}, Size: uint64(len(d.Text))}, bytes.NewBufferString(d.Text))
		if e != nil {
			return e
		}
		byName[d.Name] = rev
		labels.Categories = append(labels.Categories, search.LabelledCategory{Revision: rev, Category: d.Category})
	}
	if _, e = search.BuildSemantic(ctx, s, key, search.Extractor{}, model); e != nil {
		return e
	}
	for _, q := range corpus.Queries {
		l := search.LabelledQuery{Query: q.Query}
		for _, n := range q.Expected {
			l.Expected = append(l.Expected, byName[n])
		}
		labels.Queries = append(labels.Queries, l)
	}
	result, e := search.Evaluate(ctx, s, key, model, labels)
	if e != nil {
		return e
	}
	b, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		return e
	}
	fmt.Println(string(b))
	if *output != "" {
		if e = os.WriteFile(*output, append(b, '\n'), 0600); e != nil {
			return e
		}
	}
	if *parity != "" {
		text := "A national identity card proves who I am."
		tokens, e := model.Tokens(text)
		if e != nil {
			return e
		}
		v, e := model.EmbedTokens(ctx, tokens)
		if e != nil {
			return e
		}
		b, e := json.Marshal(struct {
			Text        string    `json:"text"`
			Tokens      []int64   `json:"tokens"`
			Vector      []float32 `json:"vector"`
			Fingerprint string    `json:"fingerprint"`
		}{text, tokens, v, model.Fingerprint()})
		if e != nil {
			return e
		}
		if e = os.WriteFile(*parity, b, 0600); e != nil {
			return e
		}
	}
	if !result.HybridPass {
		return fmt.Errorf("hybrid top-3 below 80 percent")
	}
	return nil
}
