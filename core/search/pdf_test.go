package search

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPDFRepeatedImageOCRRunsOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX counting wrapper")
	}
	for _, tool := range []string{"tesseract", "pdfinfo", "pdfimages", "pdftotext", "pdftoppm"} {
		if _, e := exec.LookPath(tool); e != nil {
			if os.Getenv("VAULT_REQUIRE_OCR") == "1" {
				t.Fatal("required OCR tools unavailable")
			}
			t.Skip("OCR tools unavailable")
		}
	}
	real, e := exec.LookPath("tesseract")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	count := filepath.Join(dir, "calls")
	wrapper := "#!/bin/sh\nprintf 'call\\n' >> '" + count + "'\nexec '" + real + "' \"$@\"\n"
	if e = os.WriteFile(filepath.Join(dir, "tesseract"), []byte(wrapper), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	data, e := os.ReadFile("testdata/repeated.pdf")
	if e != nil {
		t.Fatal(e)
	}
	text, e := (Extractor{TempDir: dir}).Text(context.Background(), "repeated.pdf", data)
	if e != nil {
		t.Fatal(e)
	}
	terms := frequencies(text)
	for _, word := range []string{"selectable", "second", "nebula", "certificate"} {
		if terms[word] == 0 {
			t.Fatal("lost embedded or scanned word")
		}
	}
	calls, e := os.ReadFile(count)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Count(string(calls), "call") != 1 {
		t.Fatal("repeated image OCR was not reused")
	}
	if terms["nebula"] != 2 {
		t.Fatal("cached image terms missing on repeated page")
	}
}

func TestPDFImageBoundsAndTextMerge(t *testing.T) {
	for _, listing := range []string{
		"1 0 image 50000 50000 rgb 3 8 jpeg no 5 0",
		"1 0 image 0 10 rgb 3 8 jpeg no 5 0",
		"2 0 image 10 10 rgb 3 8 jpeg no 5 0",
		"1 0 image invalid 10 rgb 3 8 jpeg no 5 0",
	} {
		if _, e := pdfImages([]byte(listing), 1); e == nil {
			t.Fatal("invalid image listing accepted")
		}
	}
	out := &boundedBuffer{max: 1000}
	if e := mergeOCR(out, []byte("selectable heading"), [][]byte{[]byte("SELECTABLE HEADING nebula nebula")}); e != nil {
		t.Fatal(e)
	}
	terms := frequencies(out.String())
	if terms["selectable"] != 1 || terms["heading"] != 1 || terms["nebula"] != 2 {
		t.Fatal("merged text lost or doubled terms")
	}
	if e := mergeOCR(&boundedBuffer{max: 3}, []byte("long"), nil); e != ErrLimit {
		t.Fatal("merge limit")
	}
}
