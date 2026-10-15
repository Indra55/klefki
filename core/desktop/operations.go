package desktop

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	vc "klefki/core/crypto"
	"klefki/core/local"
)

type progressReader struct {
	ctx         context.Context
	r           io.Reader
	done, total uint64
	last        time.Time
	report      func(uint64, uint64)
}

func (r *progressReader) Read(b []byte) (int, error) {
	if e := check(r.ctx); e != nil {
		return 0, e
	}
	n, e := r.r.Read(b)
	r.done += uint64(n)
	if time.Since(r.last) > 100*time.Millisecond || r.done == r.total {
		r.report(r.done, r.total)
		r.last = time.Now()
	}
	return n, e
}
func (s *Service) operation(fn func(*session, context.Context, string) error) (string, error) {
	id, e := vc.RandomID()
	if e != nil {
		return "", e
	}
	op := id.String()
	s.mu.Lock()
	x := s.session
	if x == nil {
		s.mu.Unlock()
		return "", ErrLocked
	}
	ctx, cancel := context.WithCancel(x.ctx)
	s.ops[op] = cancel
	s.mu.Unlock()
	go func() {
		defer cancel()
		e := s.with(func(current *session) error {
			if current != x {
				return ErrLocked
			}
			return fn(x, ctx, op)
		})
		s.mu.Lock()
		delete(s.ops, op)
		s.mu.Unlock()
		code := Code(e)
		if ctx.Err() != nil && x.ctx.Err() == nil {
			code = "CANCELED"
		}
		s.event(Event{Generation: x.generation, Kind: "finished", Operation: op, Error: code})
	}()
	return op, nil
}
func (s *Service) Cancel(op string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.ops[op]; cancel != nil {
		cancel()
	}
}
func (s *Service) Import(paths []string, parent *string, tags []string) (string, error) {
	if len(paths) == 0 {
		return "", ErrInvalid
	}
	return s.operation(func(x *session, ctx context.Context, op string) error {
		h, e := x.store.Heads(x.master)
		if e != nil {
			return e
		}
		if !validParent(h, "", parent) {
			return ErrInvalid
		}
		for _, tag := range tags {
			if strings.HasPrefix(tag, "klefki:system:") {
				return ErrInvalid
			}
		}
		failures := false
		imported := false
		for _, path := range paths {
			if e := check(ctx); e != nil {
				return e
			}
			name := filepath.Base(path)
			e := func() error {
				f, e := os.Open(path)
				if e != nil {
					return vc.ErrIO
				}
				defer f.Close()
				info, e := f.Stat()
				if e != nil || !info.Mode().IsRegular() || info.Size() < 0 || uint64(info.Size()) > vc.MaxFileSize {
					return ErrInvalid
				}
				obj, e := vc.RandomID()
				if e != nil {
					return e
				}
				s.event(Event{Generation: x.generation, Kind: "file-start", Operation: op, Name: name})
				r := &progressReader{ctx: ctx, r: f, total: uint64(info.Size()), report: func(done, total uint64) {
					s.event(Event{Generation: x.generation, Kind: "progress", Operation: op, Done: done, Total: total})
				}}
				_, e = x.store.PutContext(ctx, x.master, obj.String(), "", vc.Metadata{Type: "file", Name: name, Parent: parent, Tags: tags, Size: uint64(info.Size())}, r)
				return e
			}()
			if e != nil {
				failures = true
			}
			s.event(Event{Generation: x.generation, Kind: "file-finished", Operation: op, Name: name, Error: Code(e)})
			if e == nil {
				imported = true
				s.changed(x)
			}
		}
		if imported {
			if e := s.buildIndex(x, ctx, op); e != nil {
				return e
			}
		}
		if failures {
			return errors.New("IMPORT_PARTIAL")
		}
		return nil
	})
}
func (s *Service) Export(revision, path string) (string, error) {
	return s.operation(func(x *session, ctx context.Context, op string) error {
		return local.Publish(path, func(w io.Writer) error {
			if available, err := x.store.ContentAvailable(x.master, revision); err != nil {
				return err
			} else if !available {
				return errors.New("CONTENT_NOT_LOCAL")
			}
			if e := x.store.ExportContext(ctx, x.master, revision, w); e != nil {
				return e
			}
			return check(ctx)
		})
	})
}

type Preview struct {
	Generation uint64
	MIME       string
	Data       []byte
}

func (s *Service) Preview(revision string) (out Preview, e error) {
	e = s.with(func(x *session) error {
		r, e := x.store.Revision(x.master, revision)
		if e != nil {
			return e
		}
		if r.Metadata.Type != "file" || r.Metadata.Size > 64<<20 {
			return errors.New("PREVIEW_UNSUPPORTED")
		}
		ext := strings.ToLower(filepath.Ext(r.Metadata.Name))
		mime := ""
		switch ext {
		case ".pdf":
			mime = "application/pdf"
		case ".png":
			mime = "image/png"
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".webp":
			mime = "image/webp"
		case ".gif":
			mime = "image/gif"
		case ".txt", ".md", ".csv", ".json", ".log", ".yaml", ".yml":
			mime = "text/plain"
		default:
			return errors.New("PREVIEW_UNSUPPORTED")
		}
		if mime == "text/plain" && r.Metadata.Size > 2<<20 {
			return errors.New("PREVIEW_UNSUPPORTED")
		}
		var b bytes.Buffer
		if available, err := x.store.ContentAvailable(x.master, revision); err != nil {
			return err
		} else if !available {
			return errors.New("CONTENT_NOT_LOCAL")
		}
		if e = x.store.ExportContext(x.ctx, x.master, revision, &b); e != nil {
			vc.Wipe(b.Bytes())
			return e
		}
		data := b.Bytes()
		if mime == "text/plain" {
			if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
				vc.Wipe(data)
				return errors.New("PREVIEW_UNSUPPORTED")
			}
		} else if http.DetectContentType(data) != mime {
			vc.Wipe(data)
			return errors.New("PREVIEW_UNSUPPORTED")
		}
		out = Preview{x.generation, mime, data}
		return nil
	})
	if e != nil {
		vc.Wipe(out.Data)
		out = Preview{}
	}
	return
}
