package search

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	vc "vault/core/crypto"
)

const maxText = 2 << 20

var ErrTool = errors.New("SEARCH_EXTRACTION_FAILED")
var ErrUnavailable = errors.New("SEARCH_TOOL_UNAVAILABLE")

type Extractor struct {
	Language string
	// TempDir can select a private RAM-backed location for PDF staging.
	TempDir string
}

func (x Extractor) language() string {
	if x.Language == "" {
		return "eng"
	}
	return x.Language
}
func (x Extractor) Supports(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".csv", ".json", ".log", ".png", ".jpg", ".jpeg", ".pdf":
		return true
	}
	return false
}
func command(ctx context.Context, input []byte, limit int, name string, args ...string) ([]byte, error) {
	out := &boundedBuffer{max: limit}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	if e := cmd.Run(); e != nil {
		vc.Wipe(out.Bytes())
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(e, exec.ErrNotFound) {
			return nil, ErrUnavailable
		}
		if errors.Is(e, ErrLimit) {
			return nil, ErrLimit
		}
		return nil, ErrTool
	}
	return out.Bytes(), nil
}
func (x Extractor) Text(ctx context.Context, name string, data []byte) (string, error) {
	if e := ctx.Err(); e != nil {
		return "", e
	}
	if len(data) > maxDocument {
		return "", ErrLimit
	}
	lang := x.language()
	if len(lang) > 80 || strings.Trim(lang, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+") != "" {
		return "", ErrTool
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".csv", ".json", ".log":
		if len(data) > maxText {
			return "", ErrLimit
		}
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return "", ErrTool
		}
		return string(data), nil
	case ".pdf":
		return x.pdf(ctx, data, lang)
	case ".png", ".jpg", ".jpeg":
		// Tesseract also accepts lists of filenames. Decode and re-encode image
		// input so a mislabeled document cannot instruct it to open other files.
		config, _, e := image.DecodeConfig(bytes.NewReader(data))
		if e != nil {
			return "", ErrTool
		}
		if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
			return "", ErrLimit
		}
		decoded, _, e := image.Decode(bytes.NewReader(data))
		if e != nil {
			return "", ErrTool
		}
		canonical := &boundedBuffer{max: maxDocument}
		defer func() { vc.Wipe(canonical.Bytes()) }()
		if e = png.Encode(canonical, decoded); e != nil {
			return "", ErrTool
		}
		text, e := command(ctx, canonical.Bytes(), maxText, "tesseract", "stdin", "stdout", "-l", lang)
		defer vc.Wipe(text)
		if e != nil {
			return "", e
		}
		if !utf8.Valid(text) {
			return "", ErrTool
		}
		return string(text), nil
	}
	return "", ErrTool
}
