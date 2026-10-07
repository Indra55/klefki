package search

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	vc "vault/core/crypto"
)

type pdfImage struct {
	key, kind string
}

// pdfImages preserves listing order, including masks, to match pdfimages' filenames.
func pdfImages(listing []byte, pages int) (map[int][]pdfImage, error) {
	result := map[int][]pdfImage{}
	pixels := map[int]int64{}
	for _, line := range strings.Split(string(listing), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		page, e := strconv.Atoi(f[0])
		if e != nil {
			continue
		}
		if len(f) < 12 || page < 1 || page > pages {
			return nil, ErrTool
		}
		width, e := strconv.ParseInt(f[3], 10, 32)
		if e != nil {
			return nil, ErrTool
		}
		height, e := strconv.ParseInt(f[4], 10, 32)
		if e != nil {
			return nil, ErrTool
		}
		if width <= 0 || height <= 0 || width*height > 16_000_000 {
			return nil, ErrLimit
		}
		pixels[page] += width * height
		if pixels[page] > 32_000_000 || len(result[page]) >= 128 {
			return nil, ErrLimit
		}
		key := ""
		if _, e = strconv.Atoi(f[10]); e == nil {
			if _, e = strconv.Atoi(f[11]); e == nil {
				key = f[10] + ":" + f[11]
			}
		}
		result[page] = append(result[page], pdfImage{key, f[2]})
	}
	return result, nil
}

func mergeOCR(out *boundedBuffer, embedded []byte, ocr [][]byte) error {
	if !utf8.Valid(embedded) {
		return ErrTool
	}
	if _, e := out.Write(embedded); e != nil {
		return e
	}
	known := frequencies(string(embedded))
	for _, text := range ocr {
		if !utf8.Valid(text) {
			return ErrTool
		}
		for _, word := range strings.Fields(string(text)) {
			missing := false
			for term := range frequencies(word) {
				if known[term] == 0 {
					missing = true
					break
				}
			}
			if missing {
				if _, e := out.Write([]byte(" " + word)); e != nil {
					return e
				}
			}
		}
	}
	_, e := out.Write([]byte("\n"))
	return e
}

func (x Extractor) pdf(ctx context.Context, data []byte, lang string) (string, error) {
	dir, e := os.MkdirTemp(x.TempDir, "vault-ocr-*")
	if e != nil {
		return "", ErrTool
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "input.pdf")
	if os.WriteFile(path, data, 0600) != nil {
		return "", ErrTool
	}
	info, e := command(ctx, nil, 64<<10, "pdfinfo", path)
	if e != nil {
		return "", e
	}
	pages := 0
	for _, line := range strings.Split(string(info), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "Pages:" {
			pages, _ = strconv.Atoi(fields[1])
		}
	}
	vc.Wipe(info)
	if pages <= 0 || pages > 100 {
		return "", ErrLimit
	}
	listing, e := command(ctx, nil, 4<<20, "pdfimages", "-list", path)
	if e != nil {
		return "", e
	}
	images, e := pdfImages(listing, pages)
	vc.Wipe(listing)
	if e != nil {
		return "", e
	}
	// One extraction pass for all embedded text instead of launching a process per page.
	embedded, e := command(ctx, nil, maxText, "pdftotext", "-enc", "UTF-8", path, "-")
	if e != nil {
		return "", e
	}
	defer vc.Wipe(embedded)
	pageText := bytes.Split(embedded, []byte{'\f'})
	if len(pageText) == pages+1 && len(bytes.TrimSpace(pageText[pages])) == 0 {
		pageText = pageText[:pages]
	}
	if len(pageText) != pages {
		return "", ErrTool
	}
	cache := map[string][]byte{}
	defer func() {
		for _, text := range cache {
			vc.Wipe(text)
		}
	}()
	cacheBytes := 0
	out := &boundedBuffer{max: maxText}
	defer func() { vc.Wipe(out.Bytes()) }()
	for page := 1; page <= pages; page++ {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		text := pageText[page-1]
		n := strconv.Itoa(page)
		entries := images[page]
		fallback := len(frequencies(string(text))) == 0
		for _, entry := range entries {
			if entry.kind != "image" {
				fallback = true
			}
		}
		if fallback {
			// Render scans/masked composites as displayed, preserving rotation and masking.
			prefix := filepath.Join(dir, "page")
			raster, e := command(ctx, nil, 1024, "pdftoppm", "-f", n, "-l", n, "-singlefile", "-scale-to", "3000", "-png", path, prefix)
			vc.Wipe(raster)
			if e != nil {
				return "", e
			}
			recognized, e := command(ctx, nil, maxText, "tesseract", prefix+".png", "stdout", "-l", lang)
			os.Remove(prefix + ".png")
			if e != nil {
				return "", e
			}
			e = mergeOCR(out, text, [][]byte{recognized})
			vc.Wipe(recognized)
			if e != nil {
				return "", e
			}
			continue
		}
		needExtraction := false
		for _, entry := range entries {
			if _, cached := cache[entry.key]; entry.key == "" || !cached {
				needExtraction = true
			}
		}
		imageDir := filepath.Join(dir, "images")
		if needExtraction {
			if e = os.Mkdir(imageDir, 0700); e != nil {
				return "", ErrTool
			}
			output, e := command(ctx, nil, 1024, "pdfimages", "-f", n, "-l", n, "-png", path, filepath.Join(imageDir, "image"))
			vc.Wipe(output)
			if e != nil {
				return "", e
			}
		}
		ocr := [][]byte{}
		pageKeys := map[string]bool{}
		for i, entry := range entries {
			key := entry.key
			if key == "" {
				key = fmt.Sprintf("inline:%d:%d", page, i)
			}
			if pageKeys[key] {
				continue
			}
			pageKeys[key] = true
			recognized, ok := cache[key]
			if !ok {
				imagePath := filepath.Join(imageDir, fmt.Sprintf("image-%03d.png", i))
				recognized, e = command(ctx, nil, maxText, "tesseract", imagePath, "stdout", "-l", lang)
				if e != nil {
					return "", e
				}
				cacheBytes += len(recognized)
				if cacheBytes > maxText {
					vc.Wipe(recognized)
					return "", ErrLimit
				}
				cache[key] = recognized
			}
			ocr = append(ocr, recognized)
		}
		if needExtraction {
			if e = os.RemoveAll(imageDir); e != nil {
				return "", ErrTool
			}
		}
		if e = mergeOCR(out, text, ocr); e != nil {
			return "", e
		}
	}
	return out.String(), nil
}
