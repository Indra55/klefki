package server

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

var ErrConflict = errors.New("immutable blob conflict")

// BlobStore publishes complete, immutable ciphertext chunks. Implementations
// must isolate account namespaces and make identical retries idempotent.
type BlobStore interface {
	Put(account, blob string, index uint64, data []byte) (bool, error)
	Get(account, blob string, index uint64) ([]byte, error)
	List(account, blob string, after int64) ([]uint64, error)
}
type DiskStore struct{ root string }

func NewDiskStore(root string) (*DiskStore, error) {
	if e := os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	return &DiskStore{root}, nil
}
func (s *DiskStore) dir(account, blob string) (string, error) {
	if !validID(account) || !validID(blob) {
		return "", errors.New("invalid blob ID")
	}
	return filepath.Join(s.root, account, blob), nil
}
func (s *DiskStore) Put(account, blob string, index uint64, data []byte) (bool, error) {
	dir, e := s.dir(account, blob)
	if e != nil {
		return false, e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return false, e
	}
	f, e := os.CreateTemp(dir, ".upload-")
	if e != nil {
		return false, e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, e = f.Write(data); e != nil {
		return false, e
	}
	if e = f.Sync(); e != nil {
		return false, e
	}
	if e = f.Close(); e != nil {
		return false, e
	}
	target := filepath.Join(dir, strconv.FormatUint(index, 10))
	e = os.Link(f.Name(), target)
	if errors.Is(e, os.ErrExist) {
		old, e := os.ReadFile(target)
		if e != nil {
			return false, e
		}
		if !bytes.Equal(old, data) {
			return false, ErrConflict
		}
		return false, nil
	}
	if e != nil {
		return false, e
	}
	d, e := os.Open(dir)
	if e != nil {
		return false, e
	}
	defer d.Close()
	if e = d.Sync(); e != nil {
		return false, e
	}
	return true, nil
}
func (s *DiskStore) Get(account, blob string, index uint64) ([]byte, error) {
	dir, e := s.dir(account, blob)
	if e != nil {
		return nil, e
	}
	return os.ReadFile(filepath.Join(dir, strconv.FormatUint(index, 10)))
}
func (s *DiskStore) List(account, blob string, after int64) ([]uint64, error) {
	dir, e := s.dir(account, blob)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(dir)
	out := []uint64{}
	if errors.Is(e, os.ErrNotExist) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	for {
		entries, err := f.ReadDir(256)
		for _, entry := range entries {
			n, e := strconv.ParseUint(entry.Name(), 10, 64)
			if e != nil || entry.IsDir() || (after >= 0 && n <= uint64(after)) {
				continue
			}
			i := sort.Search(len(out), func(i int) bool { return out[i] >= n })
			if i >= 1000 {
				continue
			}
			out = append(out, 0)
			copy(out[i+1:], out[i:])
			out[i] = n
			if len(out) > 1000 {
				out = out[:1000]
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
