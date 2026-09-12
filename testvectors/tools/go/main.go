// M0 fixture tooling only. ALL inputs and outputs here are public synthetic data.
// Do not import this package into production or use its deterministic nonces.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const chunkSize = 4 * 1024 * 1024

var invalid = errors.New("invalid fixture record")
var account = seq(16, 0)
var object = seq(16, 16)
var master = seq(32, 32)
var recovery = seq(32, 64)
var fileKey = seq(32, 96)
var domain = []byte("vault/record/v1\x00")

type KDF struct {
	ID         string `json:"id"`
	Password   string `json:"password_utf8_hex"`
	Descriptor string `json:"descriptor_hex"`
	Output     string `json:"output_hex"`
	KEK        string `json:"kek_hex"`
	AuthKey    string `json:"auth_key_hex"`
	Credential string `json:"credential_hex"`
	Verifier   string `json:"verifier_hex"`
}
type Record struct {
	ID      string `json:"id"`
	Kind    byte   `json:"kind"`
	Account string `json:"account_hex"`
	Object  string `json:"object_hex"`
	Key     string `json:"key_hex"`
	Nonce   string `json:"nonce_hex"`
	Extra   string `json:"extra_aad_hex"`
	Plain   string `json:"plaintext_hex"`
	Index   uint64 `json:"index"`
	Final   bool   `json:"final"`
	Encoded string `json:"record_hex"`
}
type Stream struct {
	ID      string   `json:"id"`
	Length  int      `json:"length"`
	Digests []string `json:"record_sha256"`
}
type Negative struct {
	ID        string `json:"id"`
	Base      string `json:"base"`
	Operation string `json:"operation"`
}
type Vectors struct {
	Version      int        `json:"fixture_version"`
	Notice       string     `json:"notice"`
	KDF          []KDF      `json:"kdf"`
	RecoveryKey  string     `json:"recovery_key_hex"`
	RecoveryCode string     `json:"recovery_code"`
	Records      []Record   `json:"records"`
	Streams      []Stream   `json:"streams"`
	Negative     []Negative `json:"negative"`
}

func seq(n, start int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(start + i)
	}
	return b
}
func hx(b []byte) string { return hex.EncodeToString(b) }
func unhex(s string) []byte {
	b, e := hex.DecodeString(s)
	if e != nil {
		panic("invalid public fixture hex")
	}
	return b
}
func hash(b []byte) []byte { d := sha256.Sum256(b); return d[:] }
func mac(key, message []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(message)
	return h.Sum(nil)
}
func descriptor() []byte {
	b := []byte{1, 1}
	b = binary.BigEndian.AppendUint32(b, 65536)
	b = binary.BigEndian.AppendUint32(b, 3)
	b = append(b, 1)
	return append(b, seq(16, 160)...)
}
func derive(password, d []byte) ([]byte, error) {
	if len(d) != 27 || d[0] != 1 || d[1] != 1 || binary.BigEndian.Uint32(d[2:6]) != 65536 || binary.BigEndian.Uint32(d[6:10]) != 3 || d[10] != 1 || len(password) > 1024 {
		return nil, invalid
	}
	return argon2.IDKey(password, d[11:], 3, 65536, 1, 64), nil
}
func recoveryCode(key []byte) string {
	return "vlt1-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
}
func recoveryDecode(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "vlt1-") {
		return nil, invalid
	}
	b, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s[5:])
	if e != nil || len(b) != 32 || recoveryCode(b) != s {
		return nil, invalid
	}
	return b, nil
}
func seal(r Record) []byte {
	p, k, n := unhex(r.Plain), unhex(r.Key), unhex(r.Nonce)
	h := []byte{1, r.Kind}
	h = append(h, unhex(r.Account)...)
	h = append(h, unhex(r.Object)...)
	if r.Kind == 5 {
		h = binary.BigEndian.AppendUint64(h, r.Index)
		if r.Final {
			h = append(h, 1)
		} else {
			h = append(h, 0)
		}
		h = binary.BigEndian.AppendUint32(h, uint32(len(p)))
	}
	h = append(h, n...)
	aad := append(append(append([]byte{}, domain...), h...), unhex(r.Extra)...)
	a, e := chacha20poly1305.NewX(k)
	if e != nil {
		panic("fixture key")
	}
	return a.Seal(h, n, p, aad)
}
func open(b []byte, expected Record) ([]byte, error) {
	hlen := 58
	if expected.Kind == 5 {
		hlen = 71
	}
	if len(b) < hlen+16 || b[0] != 1 || b[1] != expected.Kind || !bytes.Equal(b[2:18], unhex(expected.Account)) || !bytes.Equal(b[18:34], unhex(expected.Object)) {
		return nil, invalid
	}
	plen := len(b) - hlen - 16
	if expected.Kind == 5 {
		if b[42] > 1 || binary.BigEndian.Uint64(b[34:42]) != expected.Index || (b[42] == 1) != expected.Final || int(binary.BigEndian.Uint32(b[43:47])) != plen || plen > chunkSize || (!expected.Final && plen != chunkSize) || (plen == 0 && (expected.Index != 0 || !expected.Final)) {
			return nil, invalid
		}
	} else if expected.Kind >= 1 && expected.Kind <= 3 {
		if plen != 32 {
			return nil, invalid
		}
	} else if expected.Kind != 4 || plen > 1024*1024 {
		return nil, invalid
	}
	a, e := chacha20poly1305.NewX(unhex(expected.Key))
	if e != nil {
		return nil, invalid
	}
	aad := append(append(append([]byte{}, domain...), b[:hlen]...), unhex(expected.Extra)...)
	p, e := a.Open(nil, b[hlen-24:hlen], b[hlen:], aad)
	if e != nil {
		return nil, invalid
	}
	return p, nil
}
func newRecord(id string, kind byte, key, plain []byte) Record {
	obj := object
	if kind == 1 || kind == 2 {
		obj = make([]byte, 16)
	}
	return Record{ID: id, Kind: kind, Account: hx(account), Object: hx(obj), Key: hx(key), Nonce: hx(hash([]byte("vault-fixture/" + id))[:24]), Plain: hx(plain)}
}
func stream(length int) ([]Record, [][]byte) {
	count := (length + chunkSize - 1) / chunkSize
	if count == 0 {
		count = 1
	}
	rs := make([]Record, count)
	bs := make([][]byte, count)
	for i := 0; i < count; i++ {
		n := length - i*chunkSize
		if n > chunkSize {
			n = chunkSize
		}
		p := make([]byte, n)
		for j := range p {
			p[j] = byte((i*chunkSize + j) % 251)
		}
		r := newRecord(fmt.Sprintf("stream-%d-%d", length, i), 5, fileKey, p)
		r.Index = uint64(i)
		r.Final = i == count-1
		rs[i] = r
		bs[i] = seal(r)
	}
	return rs, bs
}
func checkStream(bs [][]byte, length int, key []byte) error {
	count := (length + chunkSize - 1) / chunkSize
	if count == 0 {
		count = 1
	}
	if len(bs) != count {
		return invalid
	}
	total := 0
	for i, b := range bs {
		r := newRecord("unused", 5, key, nil)
		r.Index = uint64(i)
		r.Final = i == count-1
		p, e := open(b, r)
		if e != nil {
			return invalid
		}
		for j, x := range p {
			if x != byte((i*chunkSize+j)%251) {
				return invalid
			}
		}
		total += len(p)
	}
	if total != length {
		return invalid
	}
	return nil
}
func build() Vectors {
	v := Vectors{Version: 1, Notice: "PUBLIC SYNTHETIC FIXTURES ONLY. Never use these passwords, keys or nonces for real data.", RecoveryKey: hx(recovery), RecoveryCode: recoveryCode(recovery)}
	for i, p := range []string{"vault synthetic password", "Väult 🔐 e\u0301", ""} {
		d := descriptor()
		key, e := derive([]byte(p), d)
		if e != nil {
			panic("fixture kdf")
		}
		c := mac(key[32:], append([]byte("vault/auth/v1\x00"), account...))
		verifier := hash(append([]byte("vault/auth-verifier/v1\x00"), c...))
		v.KDF = append(v.KDF, KDF{ID: fmt.Sprintf("kdf-%d", i), Password: hx([]byte(p)), Descriptor: hx(d), Output: hx(key), KEK: hx(key[:32]), AuthKey: hx(key[32:]), Credential: hx(c), Verifier: hx(verifier)})
	}
	r := newRecord("master-password", 1, unhex(v.KDF[0].KEK), master)
	r.Extra = hx(descriptor())
	v.Records = append(v.Records, r, newRecord("master-recovery", 2, recovery, master), newRecord("file-key", 3, master, fileKey))
	metadata := []byte(`{"type":"file","name":"Certificate 🔐.pdf","parent":null,"tags":["fixture"],"favorite":false,"file_id":"101112131415161718191a1b1c1d1e1f","size":0,"chunk_count":1}`)
	v.Records = append(v.Records, newRecord("metadata-unicode", 4, master, metadata))
	for _, p := range []struct {
		id string
		b  []byte
	}{{"chunk-empty", nil}, {"chunk-binary", []byte{0, 255, 1, 128, 10}}, {"chunk-unicode", []byte("scan प्रमाणपत्र")}} {
		r := newRecord(p.id, 5, fileKey, p.b)
		r.Final = true
		v.Records = append(v.Records, r)
	}
	for i := range v.Records {
		v.Records[i].Encoded = hx(seal(v.Records[i]))
	}
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 2 * chunkSize} {
		_, bs := stream(n)
		s := Stream{ID: fmt.Sprintf("stream-%d", n), Length: n}
		for _, b := range bs {
			s.Digests = append(s.Digests, hx(hash(b)))
		}
		v.Streams = append(v.Streams, s)
	}
	for _, op := range []string{"flip-ciphertext", "flip-nonce", "wrong-key", "wrong-account", "wrong-object", "wrong-kind", "unknown-version", "short-record", "trailing-byte", "wrong-index", "wrong-final", "wrong-length", "invalid-final"} {
		v.Negative = append(v.Negative, Negative{ID: "reject-" + op, Base: "chunk-binary", Operation: op})
	}
	v.Negative = append(v.Negative, Negative{ID: "reject-kdf-aad", Base: "master-password", Operation: "wrong-extra"})
	for _, op := range []string{"reorder", "drop-first", "drop-final", "truncate-final", "append-record", "mix-revision"} {
		v.Negative = append(v.Negative, Negative{ID: "reject-stream-" + op, Base: "stream-4194305", Operation: op})
	}
	for _, op := range []string{"kdf-memory", "kdf-time", "kdf-lanes", "kdf-version", "kdf-short"} {
		v.Negative = append(v.Negative, Negative{ID: "reject-" + op, Base: "kdf-0", Operation: op})
	}
	return v
}
func reject(v Vectors, n Negative) bool {
	if n.Base == "kdf-0" {
		d := unhex(v.KDF[0].Descriptor)
		switch n.Operation {
		case "kdf-memory":
			d[2] = 255
		case "kdf-time":
			d[9] = 0
		case "kdf-lanes":
			d[10] = 2
		case "kdf-version":
			d[0] = 2
		case "kdf-short":
			d = d[:26]
		default:
			return false
		}
		_, e := derive(unhex(v.KDF[0].Password), d)
		return e != nil
	}
	if strings.HasPrefix(n.Base, "stream-") {
		rs, bs := stream(chunkSize + 1)
		switch n.Operation {
		case "reorder":
			bs[0], bs[1] = bs[1], bs[0]
		case "drop-first":
			bs = bs[1:]
		case "drop-final":
			bs = bs[:1]
		case "truncate-final":
			bs[1] = bs[1][:len(bs[1])-1]
		case "append-record":
			bs = append(bs, bs[1])
		case "mix-revision":
			rs[1].Key = hx(seq(32, 128))
			bs[1] = seal(rs[1])
		default:
			return false
		}
		return checkStream(bs, chunkSize+1, fileKey) != nil
	}
	var r Record
	found := false
	for _, x := range v.Records {
		if x.ID == n.Base {
			r = x
			found = true
		}
	}
	if !found {
		return false
	}
	b := unhex(r.Encoded)
	switch n.Operation {
	case "flip-ciphertext":
		b[len(b)-1] ^= 1
	case "flip-nonce":
		b[47] ^= 1
	case "wrong-key":
		r.Key = hx(seq(32, 200))
	case "wrong-account":
		r.Account = hx(seq(16, 200))
	case "wrong-object":
		r.Object = hx(seq(16, 200))
	case "wrong-kind":
		r.Kind = 4
	case "unknown-version":
		b[0] = 2
	case "short-record":
		b = b[:12]
	case "trailing-byte":
		b = append(b, 0)
	case "wrong-index":
		b[41] = 1
	case "wrong-final":
		b[42] = 0
	case "wrong-length":
		b[46] ^= 1
	case "invalid-final":
		b[42] = 2
	case "wrong-extra":
		d := unhex(r.Extra)
		d[26] ^= 1
		r.Extra = hx(d)
	default:
		return false
	}
	_, e := open(b, r)
	return e != nil
}
func verify(path string) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return errors.New("cannot read vector file")
	}
	var v Vectors
	if json.Unmarshal(b, &v) != nil {
		return errors.New("invalid vector JSON")
	}
	if !reflect.DeepEqual(v, build()) {
		return errors.New("fixture reproduction failed")
	}
	for _, r := range v.Records {
		p, e := open(unhex(r.Encoded), r)
		if e != nil || !bytes.Equal(p, unhex(r.Plain)) {
			return fmt.Errorf("FAIL %s", r.ID)
		}
	}
	for _, s := range v.Streams {
		_, bs := stream(s.Length)
		if checkStream(bs, s.Length, fileKey) != nil {
			return fmt.Errorf("FAIL %s", s.ID)
		}
	}
	k, e := recoveryDecode(v.RecoveryCode)
	if e != nil || !bytes.Equal(k, recovery) {
		return errors.New("FAIL recovery")
	}
	for _, n := range v.Negative {
		if !reject(v, n) {
			return fmt.Errorf("FAIL %s", n.ID)
		}
	}
	return nil
}
func main() {
	write := flag.Bool("write", false, "regenerate PUBLIC synthetic fixtures")
	path := flag.String("file", "../../vectors-v1.json", "fixture path")
	flag.Parse()
	if *write {
		b, e := json.MarshalIndent(build(), "", "  ")
		if e != nil || os.WriteFile(*path, append(b, '\n'), 0644) != nil {
			fmt.Fprintln(os.Stderr, "FAIL generate")
			os.Exit(1)
		}
		fmt.Println("PASS generate")
		return
	}
	if e := verify(*path); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	fmt.Println("PASS Go vectors")
}
