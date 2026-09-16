// Package local provides shared account containers and private file publication.
package local

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	vc "vault/core/crypto"
)

var ErrExists = errors.New("destination already exists")
var ErrInUse = errors.New("vault is open in another process")

type Lease struct{ f *os.File }

func Acquire(path string) (*Lease, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, vc.ErrIO
	}
	if lockFile(f) != nil {
		f.Close()
		return nil, ErrInUse
	}
	return &Lease{f}, nil
}
func (l *Lease) Close() {
	if l != nil && l.f != nil {
		unlockFile(l.f)
		l.f.Close()
		l.f = nil
	}
}
func AccountBytes(a vc.Account) []byte {
	b := append([]byte("VLTA\x01"), a.ID[:]...)
	b = append(b, a.Descriptor...)
	b = append(b, a.PasswordWrapper...)
	return append(b, a.RecoveryWrapper...)
}
func ReadAccount(r io.Reader) (a vc.Account, e error) {
	b, e := io.ReadAll(io.LimitReader(r, 261))
	if e != nil || len(b) != 260 || !bytes.Equal(b[:5], []byte("VLTA\x01")) {
		return a, vc.ErrInvalid
	}
	copy(a.ID[:], b[5:21])
	a.Descriptor = b[21:48]
	a.PasswordWrapper = b[48:154]
	a.RecoveryWrapper = b[154:260]
	valid := func(b []byte, k vc.Kind) bool {
		return len(b) == 106 && b[0] == 1 && b[1] == byte(k) && vc.ID(b[2:18]) == a.ID && vc.ID(b[18:34]) == (vc.ID{})
	}
	if a.ID == (vc.ID{}) || vc.ValidateDescriptor(a.Descriptor) != nil || !valid(a.PasswordWrapper, vc.PasswordMaster) || !valid(a.RecoveryWrapper, vc.RecoveryMaster) {
		return vc.Account{}, vc.ErrInvalid
	}
	return a, nil
}
func LoadAccount(path string) (vc.Account, error) {
	f, e := os.Open(path)
	if e != nil {
		return vc.Account{}, vc.ErrIO
	}
	defer f.Close()
	return ReadAccount(f)
}
func Publish(path string, fill func(io.Writer) error) error {
	if _, e := os.Lstat(path); e == nil {
		return ErrExists
	} else if !os.IsNotExist(e) {
		return vc.ErrIO
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".vault-stage-*")
	if e != nil {
		return vc.ErrIO
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = fill(f); e != nil {
		return e
	}
	if f.Sync() != nil || f.Close() != nil {
		return vc.ErrIO
	}
	if e = os.Link(f.Name(), path); e != nil {
		if os.IsExist(e) {
			return ErrExists
		}
		return vc.ErrIO
	}
	return nil
}
