// Package crypto implements Vault v1 records. It does not implement crypto primitives.
package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"runtime"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	ChunkSize          = 4 * 1024 * 1024
	MaxMetadata        = 1024 * 1024
	MaxFileSize uint64 = 1<<53 - 1
)

type Key [32]byte
type ID [16]byte
type Kind byte

const (
	PasswordMaster Kind = 1
	RecoveryMaster Kind = 2
	FileKey        Kind = 3
	MetadataRecord Kind = 4
	Chunk          Kind = 5
)

var (
	ErrInvalid = errors.New("invalid vault data")
	ErrAuth    = errors.New("vault authentication failed")
	ErrIO      = errors.New("vault I/O failed")
)
var recordDomain = []byte("vault/record/v1\x00")

type Context struct {
	Kind            Kind
	Account, Object ID
	Index           uint64
	Final           bool
}
type Keys struct{ KEK, Auth Key }

func (k *Keys) Clear() { Wipe(k.KEK[:]); Wipe(k.Auth[:]) }

// Wipe is best effort; runtimes and callers may hold other copies.
func Wipe(b []byte)       { clear(b); runtime.KeepAlive(b) }
func Equal(a, b Key) bool { return subtle.ConstantTimeCompare(a[:], b[:]) == 1 }
func RandomKey() (k Key, e error) {
	if _, e = rand.Read(k[:]); e != nil {
		return Key{}, ErrIO
	}
	return
}
func RandomID() (id ID, e error) {
	for id == (ID{}) {
		if _, e = rand.Read(id[:]); e != nil {
			return ID{}, ErrIO
		}
	}
	return
}
func NewDescriptor() ([]byte, error) {
	d := make([]byte, 27)
	d[0] = 1
	d[1] = 1
	binary.BigEndian.PutUint32(d[2:6], 65536)
	binary.BigEndian.PutUint32(d[6:10], 3)
	d[10] = 1
	if _, e := rand.Read(d[11:]); e != nil {
		return nil, ErrIO
	}
	return d, nil
}
func ValidateDescriptor(d []byte) error {
	if len(d) != 27 || d[0] != 1 || d[1] != 1 || binary.BigEndian.Uint32(d[2:6]) != 65536 || binary.BigEndian.Uint32(d[6:10]) != 3 || d[10] != 1 {
		return ErrInvalid
	}
	return nil
}
func Derive(password, d []byte) (out Keys, e error) {
	if ValidateDescriptor(d) != nil || len(password) > 1024 || !utf8.Valid(password) {
		return out, ErrInvalid
	}
	b := argon2.IDKey(password, d[11:], 3, 65536, 1, 64)
	defer Wipe(b)
	copy(out.KEK[:], b[:32])
	copy(out.Auth[:], b[32:])
	return
}
func Credential(auth Key, account ID) (out Key) {
	defer Wipe(auth[:])
	m := hmac.New(sha256.New, auth[:])
	m.Write([]byte("vault/auth/v1\x00"))
	m.Write(account[:])
	copy(out[:], m.Sum(nil))
	return
}
func Verifier(credential Key) Key {
	defer Wipe(credential[:])
	m := sha256.New()
	m.Write([]byte("vault/auth-verifier/v1\x00"))
	m.Write(credential[:])
	var out Key
	copy(out[:], m.Sum(nil))
	return out
}
func RecoveryCode(k Key) string {
	defer Wipe(k[:])
	return "vlt1-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(k[:])
}
func ParseRecoveryCode(s string) (k Key, e error) {
	if len(s) != 57 || !strings.HasPrefix(s, "vlt1-") {
		return k, ErrInvalid
	}
	b, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s[5:])
	defer Wipe(b)
	if e != nil || len(b) != 32 {
		return k, ErrInvalid
	}
	copy(k[:], b)
	if RecoveryCode(k) != s {
		Wipe(k[:])
		return k, ErrInvalid
	}
	return k, nil
}
func validate(c Context, plen int, extra []byte) error {
	if c.Account == (ID{}) || plen < 0 {
		return ErrInvalid
	}
	if c.Kind == PasswordMaster || c.Kind == RecoveryMaster {
		if c.Object != (ID{}) {
			return ErrInvalid
		}
	} else if c.Object == (ID{}) {
		return ErrInvalid
	}
	if c.Kind == PasswordMaster {
		if ValidateDescriptor(extra) != nil {
			return ErrInvalid
		}
	} else if len(extra) != 0 {
		return ErrInvalid
	}
	if c.Kind != Chunk && (c.Index != 0 || c.Final) {
		return ErrInvalid
	}
	switch c.Kind {
	case PasswordMaster, RecoveryMaster, FileKey:
		if plen != 32 {
			return ErrInvalid
		}
	case MetadataRecord:
		if plen > MaxMetadata {
			return ErrInvalid
		}
	case Chunk:
		if plen > ChunkSize || c.Index >= ChunkCount(MaxFileSize) || (!c.Final && plen != ChunkSize) || (plen == 0 && (c.Index != 0 || !c.Final)) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Seal generates a new random nonce. Deterministic nonce injection is private to tests.
func Seal(k Key, c Context, p, extra []byte) ([]byte, error) {
	defer Wipe(k[:])
	if validate(c, len(p), extra) != nil {
		return nil, ErrInvalid
	}
	n := make([]byte, 24)
	if _, e := rand.Read(n); e != nil {
		return nil, ErrIO
	}
	return sealNonce(k, c, p, extra, n)
}
func sealNonce(k Key, c Context, p, extra, n []byte) ([]byte, error) {
	defer Wipe(k[:])
	if len(n) != 24 || validate(c, len(p), extra) != nil {
		return nil, ErrInvalid
	}
	h := make([]byte, 0, 71)
	h = append(h, 1, byte(c.Kind))
	h = append(h, c.Account[:]...)
	h = append(h, c.Object[:]...)
	if c.Kind == Chunk {
		h = binary.BigEndian.AppendUint64(h, c.Index)
		if c.Final {
			h = append(h, 1)
		} else {
			h = append(h, 0)
		}
		h = binary.BigEndian.AppendUint32(h, uint32(len(p)))
	}
	h = append(h, n...)
	aad := make([]byte, 0, len(recordDomain)+len(h)+len(extra))
	aad = append(aad, recordDomain...)
	aad = append(aad, h...)
	aad = append(aad, extra...)
	a, _ := chacha20poly1305.NewX(k[:])
	return a.Seal(h, n, p, aad), nil
}

// Open authenticates a record against caller-supplied identity and context.
func Open(k Key, c Context, b, extra []byte) ([]byte, error) {
	defer Wipe(k[:])
	hlen := 58
	if c.Kind == Chunk {
		hlen = 71
	}
	if len(b) < hlen+16 || validate(c, len(b)-hlen-16, extra) != nil {
		return nil, ErrInvalid
	}
	if b[0] != 1 || Kind(b[1]) != c.Kind || ID(b[2:18]) != c.Account || ID(b[18:34]) != c.Object {
		return nil, ErrInvalid
	}
	if c.Kind == Chunk && (binary.BigEndian.Uint64(b[34:42]) != c.Index || b[42] > 1 || (b[42] == 1) != c.Final || uint64(binary.BigEndian.Uint32(b[43:47])) != uint64(len(b)-87)) {
		return nil, ErrInvalid
	}
	aad := make([]byte, 0, len(recordDomain)+hlen+len(extra))
	aad = append(aad, recordDomain...)
	aad = append(aad, b[:hlen]...)
	aad = append(aad, extra...)
	a, _ := chacha20poly1305.NewX(k[:])
	p, e := a.Open(nil, b[hlen-24:hlen], b[hlen:], aad)
	if e != nil {
		return nil, ErrAuth
	}
	return p, nil
}

type Account struct {
	ID                               ID
	Descriptor                       []byte
	PasswordWrapper, RecoveryWrapper []byte
}

func NewAccount(password []byte) (a Account, master, recovery Key, e error) {
	if len(password) == 0 || len(password) > 1024 || !utf8.Valid(password) {
		return a, master, recovery, ErrInvalid
	}
	defer func() {
		if e != nil {
			Wipe(master[:])
			Wipe(recovery[:])
		}
	}()
	if a.ID, e = RandomID(); e != nil {
		return
	}
	if a.Descriptor, e = NewDescriptor(); e != nil {
		return
	}
	var keys Keys
	if keys, e = Derive(password, a.Descriptor); e != nil {
		return
	}
	defer keys.Clear()
	if master, e = RandomKey(); e != nil {
		return
	}
	if recovery, e = RandomKey(); e != nil {
		return
	}
	if a.PasswordWrapper, e = Seal(keys.KEK, Context{Kind: PasswordMaster, Account: a.ID}, master[:], a.Descriptor); e != nil {
		return
	}
	a.RecoveryWrapper, e = Seal(recovery, Context{Kind: RecoveryMaster, Account: a.ID}, master[:], nil)
	return
}
func (a Account) Unlock(password []byte) (Key, error) {
	k, e := Derive(password, a.Descriptor)
	if e != nil {
		return Key{}, e
	}
	defer k.Clear()
	return unwrap(k.KEK, Context{Kind: PasswordMaster, Account: a.ID}, a.PasswordWrapper, a.Descriptor)
}
func (a Account) Recover(code string) (Key, error) {
	k, e := ParseRecoveryCode(code)
	if e != nil {
		return Key{}, e
	}
	defer Wipe(k[:])
	return unwrap(k, Context{Kind: RecoveryMaster, Account: a.ID}, a.RecoveryWrapper, nil)
}
func unwrap(k Key, c Context, b, extra []byte) (out Key, e error) {
	defer Wipe(k[:])
	p, e := Open(k, c, b, extra)
	defer Wipe(p)
	if e != nil {
		return out, e
	}
	copy(out[:], p)
	return
}
