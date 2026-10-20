package desktop

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
	"klefki/core/local"
	vs "klefki/core/sync"
)

type archiveEntry struct {
	item Item
	name string
}

var reservedArchiveName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\.|$)`)

func archiveName(name string, folder bool, used map[string]bool) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`/\<>:"|?*`, r) {
			return '_'
		}
		return r
	}, norm.NFC.String(name))
	name = strings.TrimRight(strings.TrimSpace(name), ". ")
	if name == "" {
		name = "Untitled"
	}
	if reservedArchiveName.MatchString(name) {
		name = "_" + name
	}
	base, ext := name, ""
	if index := strings.LastIndex(name, "."); !folder && index > 0 {
		base, ext = name[:index], name[index:]
	}
	for n := 2; used[strings.ToLower(name)]; n++ {
		name = base + " (" + strconv.Itoa(n) + ")" + ext
	}
	used[strings.ToLower(name)] = true
	return name
}

func exportEntries(items []Item, revisions []string) ([]archiveEntry, error) {
	if len(revisions) == 0 {
		return nil, ErrInvalid
	}
	byRevision, children := map[string]Item{}, map[string][]Item{}
	for _, item := range items {
		byRevision[item.Revision] = item
		if item.Parent != nil {
			children[*item.Parent] = append(children[*item.Parent], item)
		}
	}
	type queued struct {
		item   Item
		parent string
	}
	queue := []queued{}
	var source *string
	for n, revision := range revisions {
		item, exists := byRevision[revision]
		if !exists {
			return nil, vs.ErrConflict
		}
		if n == 0 {
			source = item.Parent
		} else if !sameParent(source, item.Parent) {
			return nil, ErrInvalid
		}
		queue = append(queue, queued{item, ""})
	}
	seen, names := map[string]bool{}, map[string]map[string]bool{}
	entries := []archiveEntry{}
	for n := 0; n < len(queue); n++ {
		next := queue[n]
		item := next.item
		if item.Trashed || item.TrashRoot || item.Conflict || seen[item.Object] {
			return nil, ErrInvalid
		}
		seen[item.Object] = true
		if names[next.parent] == nil {
			names[next.parent] = map[string]bool{}
		}
		name := next.parent + archiveName(item.Name, item.Type == "folder", names[next.parent])
		if item.Type == "folder" {
			name += "/"
			for _, child := range children[item.Object] {
				queue = append(queue, queued{child, name})
			}
		} else if item.Type != "file" {
			return nil, ErrInvalid
		}
		if len(name) > 65535 {
			return nil, ErrInvalid
		}
		entries = append(entries, archiveEntry{item, name})
	}
	return entries, nil
}

func (s *Service) ExportSelection(revisions []string, path string) (string, error) {
	if len(revisions) == 0 {
		return "", ErrInvalid
	}
	revisions = slices.Clone(revisions)
	return s.operation(func(x *session, ctx context.Context, op string) error {
		heads, err := x.store.AllHeads(x.master)
		if err != nil {
			return err
		}
		entries, err := exportEntries(items(heads), revisions)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.item.Type != "file" {
				continue
			}
			if available, err := x.store.ContentAvailable(x.master, entry.item.Revision); err != nil {
				return err
			} else if !available {
				return errors.New("EXPORT_CONTENT_NOT_LOCAL")
			}
		}
		return local.Publish(path, func(out io.Writer) error {
			archive := zip.NewWriter(out)
			for n, entry := range entries {
				if err := check(ctx); err != nil {
					return err
				}
				header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
				if entry.item.Type == "folder" {
					header.SetMode(0700 | fs.ModeDir)
				} else {
					header.SetMode(0600)
				}
				writer, err := archive.CreateHeader(header)
				if err != nil {
					return err
				}
				if entry.item.Type == "file" {
					if err = x.store.ExportContext(ctx, x.master, entry.item.Revision, writer); err != nil {
						return err
					}
				}
				s.event(Event{Generation: x.generation, Kind: "progress", Operation: op, Done: uint64(n + 1), Total: uint64(len(entries))})
			}
			if err := archive.Close(); err != nil {
				return err
			}
			return check(ctx)
		})
	})
}
