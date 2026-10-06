package search

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestExtractionBoundsAndFailures(t *testing.T) {
	x := Extractor{}
	ctx := context.Background()
	if text, e := x.Text(ctx, "plain.txt", []byte("café certificate")); e != nil || text != "café certificate" {
		t.Fatal("text extraction", e)
	}
	for _, data := range [][]byte{{0xff}, {'a', 0, 'b'}, make([]byte, maxText+1)} {
		if _, e := x.Text(ctx, "plain.txt", data); e == nil {
			t.Fatal("invalid/oversize text accepted")
		}
	}
	if x.Supports("file.exe") {
		t.Fatal("unsupported content enabled")
	}
	if _, e := (Extractor{Language: "--bad"}).Text(ctx, "file.png", nil); e == nil {
		t.Fatal("language argument accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := x.Text(canceled, "file.png", nil); !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation", e)
	}
	t.Setenv("PATH", t.TempDir())
	data, e := os.ReadFile("testdata/scan.png")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := x.Text(ctx, "file.png", data); !errors.Is(e, ErrUnavailable) {
		t.Fatal("missing OCR not reported", e)
	}
	if _, e := x.Text(ctx, "file.png", []byte("/private/file.png\nhttps://example.invalid/image\n")); !errors.Is(e, ErrTool) {
		t.Fatal("filename list reached OCR", e)
	}
	b := &boundedBuffer{max: 2}
	if _, e := b.Write([]byte("too long")); !errors.Is(e, ErrLimit) || b.Len() != 0 {
		t.Fatal("output limit")
	}
}
