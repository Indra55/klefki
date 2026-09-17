package crypto

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// All values in these tests are public synthetic fixtures.
func decode(s string) []byte {
	b, e := hex.DecodeString(s)
	if e != nil {
		panic("fixture hex")
	}
	return b
}
func key(b []byte) Key { var k Key; copy(k[:], b); return k }
func id(b []byte) ID   { var x ID; copy(x[:], b); return x }
func fixtures(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, e := os.ReadFile("../../testvectors/vectors-v1.json")
	if e != nil {
		t.Fatal("fixture read")
	}
	var v map[string]json.RawMessage
	if json.Unmarshal(b, &v) != nil {
		t.Fatal("fixture JSON")
	}
	return v
}

type recordFixture struct {
	ID      string `json:"id"`
	Kind    Kind   `json:"kind"`
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

func TestSharedKDFAndRecords(t *testing.T) {
	v := fixtures(t)
	var ks []struct {
		Password   string `json:"password_utf8_hex"`
		Descriptor string `json:"descriptor_hex"`
		Output     string `json:"output_hex"`
		Credential string `json:"credential_hex"`
		Verifier   string `json:"verifier_hex"`
	}
	if json.Unmarshal(v["kdf"], &ks) != nil || len(ks) != 3 {
		t.Fatal("KDF fixture schema")
	}
	account := id(decode("000102030405060708090a0b0c0d0e0f"))
	for i, x := range ks {
		k, e := Derive(decode(x.Password), decode(x.Descriptor))
		if e != nil || !bytes.Equal(append(k.KEK[:], k.Auth[:]...), decode(x.Output)) {
			t.Fatalf("KDF fixture %d", i)
		}
		c := Credential(k.Auth, account)
		ver := Verifier(c)
		if !bytes.Equal(c[:], decode(x.Credential)) || !bytes.Equal(ver[:], decode(x.Verifier)) {
			t.Fatalf("auth fixture %d", i)
		}
		k.Clear()
	}
	var rs []recordFixture
	if json.Unmarshal(v["records"], &rs) != nil || len(rs) != 7 {
		t.Fatal("record fixture schema")
	}
	for _, r := range rs {
		t.Run(r.ID, func(t *testing.T) {
			ctx := Context{Kind: r.Kind, Account: id(decode(r.Account)), Object: id(decode(r.Object)), Index: r.Index, Final: r.Final}
			k := key(decode(r.Key))
			p := decode(r.Plain)
			b, e := sealNonce(k, ctx, p, decode(r.Extra), decode(r.Nonce))
			if e != nil || !bytes.Equal(b, decode(r.Encoded)) {
				t.Fatal("record bytes mismatch")
			}
			out, e := Open(k, ctx, b, decode(r.Extra))
			if e != nil || !bytes.Equal(out, p) {
				t.Fatal("record decrypt")
			}
			b[len(b)-1] ^= 1
			if _, e = Open(k, ctx, b, decode(r.Extra)); e == nil {
				t.Fatal("tamper accepted")
			}
		})
	}
}
func TestAccountRecovery(t *testing.T) {
	a, m, r, e := NewAccount([]byte("public synthetic password"))
	if e != nil {
		t.Fatal(e)
	}
	defer Wipe(m[:])
	defer Wipe(r[:])
	got, e := a.Unlock([]byte("public synthetic password"))
	if e != nil || !Equal(got, m) {
		t.Fatal("password unlock")
	}
	got, e = a.Recover(RecoveryCode(r))
	if e != nil || !Equal(got, m) {
		t.Fatal("recovery unlock")
	}
	if _, e = a.Unlock([]byte("incorrect fixture")); e == nil {
		t.Fatal("wrong password")
	}
	wrong := r
	wrong[0] ^= 1
	if _, e = a.Recover(RecoveryCode(wrong)); e == nil {
		t.Fatal("wrong recovery")
	}
	f, e := RandomKey()
	if e != nil {
		t.Fatal(e)
	}
	fid, _ := RandomID()
	wrapped, e := Seal(m, Context{Kind: FileKey, Account: a.ID, Object: fid}, f[:], nil)
	if e != nil {
		t.Fatal(e)
	}
	out, e := Open(got, Context{Kind: FileKey, Account: a.ID, Object: fid}, wrapped, nil)
	if e != nil || !bytes.Equal(out, f[:]) {
		t.Fatal("recovered file key")
	}
	if _, _, _, e = NewAccount(nil); e == nil {
		t.Fatal("empty signup password")
	}
}
func TestStreamRoundTripAndTamper(t *testing.T) {
	k, _ := RandomKey()
	a, _ := RandomID()
	f, _ := RandomID()
	for _, n := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 2 * ChunkSize} {
		p := bytes.Repeat([]byte{17}, n)
		var encrypted, plain bytes.Buffer
		if e := EncryptStream(k, a, f, uint64(n), bytes.NewReader(p), &encrypted); e != nil {
			t.Fatal(e)
		}
		b := encrypted.Bytes()
		if e := DecryptStream(k, a, f, uint64(n), bytes.NewReader(b), &plain); e != nil || !bytes.Equal(p, plain.Bytes()) {
			t.Fatal("stream round trip")
		}
		for _, bad := range [][]byte{b[:len(b)-1], append(append([]byte{}, b...), 0)} {
			if e := DecryptStream(k, a, f, uint64(n), bytes.NewReader(bad), io.Discard); e == nil {
				t.Fatal("truncation or suffix accepted")
			}
		}
		if n == ChunkSize+1 {
			cut := ChunkSize + 87
			swapped := append(append([]byte{}, b[cut:]...), b[:cut]...)
			if DecryptStream(k, a, f, uint64(n), bytes.NewReader(swapped), io.Discard) == nil {
				t.Fatal("reorder accepted")
			}
			if DecryptStream(k, a, f, uint64(n), bytes.NewReader(b[:cut]), io.Discard) == nil {
				t.Fatal("missing final accepted")
			}
		}
	}
	var out bytes.Buffer
	if EncryptStream(k, a, f, 1, bytes.NewReader([]byte{1, 2}), &out) == nil {
		t.Fatal("source growth accepted")
	}
	if EncryptStream(k, a, f, 2, bytes.NewReader([]byte{1}), &out) == nil {
		t.Fatal("source shrink accepted")
	}
}
func TestMetadataValidation(t *testing.T) {
	good := []byte(`{"type":"file","name":"fixture.pdf","parent":null,"tags":[],"favorite":false,"file_id":"101112131415161718191a1b1c1d1e1f","size":0,"chunk_count":1}`)
	if _, e := ParseMetadata(good); e != nil {
		t.Fatal(e)
	}
	for _, bad := range [][]byte{
		bytes.Replace(good, []byte(`"size":0`), []byte(`"size":-1`), 1),
		bytes.Replace(good, []byte(`"chunk_count":1`), []byte(`"chunk_count":0`), 1),
		bytes.Replace(good, []byte(`"favorite":false`), []byte(`"favorite":false,"favorite":true`), 1),
		bytes.Replace(good, []byte(`fixture.pdf`), []byte(`\ud800`), 1),
		bytes.Replace(good, []byte(`fixture.pdf`), []byte{255}, 1),
		bytes.Replace(good, []byte(`"tags":[]`), []byte(`"tags":null`), 1),
		bytes.Replace(good, []byte(`"size":0`), []byte(`"size":9007199254740992`), 1),
		append(append([]byte{}, good...), []byte(`{}`)...),
	} {
		if _, e := ParseMetadata(bad); e == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}
func TestDescriptorAndRecoveryReject(t *testing.T) {
	for _, s := range []string{"", "vlt1-", "vlt1-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB", "VLT1-AAAA"} {
		if _, e := ParseRecoveryCode(s); e == nil {
			t.Fatal("invalid recovery accepted")
		}
	}
	d, _ := NewDescriptor()
	for _, i := range []int{0, 1, 2, 6, 10} {
		bad := append([]byte{}, d...)
		bad[i] ^= 255
		if _, e := Derive([]byte("fixture"), bad); e == nil {
			t.Fatal("bad descriptor")
		}
	}
	if _, e := Derive([]byte{255}, d); e == nil {
		t.Fatal("invalid password UTF8")
	}
}
func FuzzRecordParser(f *testing.F) {
	valid, _ := sealNonce(Key{}, Context{Kind: MetadataRecord, Account: ID{1}, Object: ID{2}}, []byte(`{"fixture":true}`), nil, make([]byte, 24))
	f.Add(valid)
	chunk, _ := sealNonce(Key{}, Context{Kind: Chunk, Account: ID{1}, Object: ID{2}, Final: true}, []byte{1, 2, 3}, nil, make([]byte, 24))
	f.Add(chunk)
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{1}, 106))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > ChunkSize+87 {
			return
		}
		_, _ = Open(Key{}, Context{Kind: MetadataRecord, Account: ID{1}, Object: ID{2}}, b, nil)
		_, _ = Open(Key{}, Context{Kind: Chunk, Account: ID{1}, Object: ID{2}, Final: true}, b, nil)
	})
}
func FuzzMetadataParser(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"name":"\ud800"}`))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = ParseMetadata(b) })
}
func FuzzRecoveryParser(f *testing.F) {
	f.Add("vlt1-")
	f.Fuzz(func(t *testing.T, s string) { _, _ = ParseRecoveryCode(s) })
}
func FuzzDescriptorParser(f *testing.F) {
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) { _ = ValidateDescriptor(b) })
}

func TestMetadataExactFieldsAndUnicode(t *testing.T) {
	base := []byte(`{"type":"file","name":"fixture","parent":null,"tags":[],"favorite":false,"file_id":"101112131415161718191a1b1c1d1e1f","size":0,"chunk_count":1}`)
	bad := bytes.Replace(base, []byte(`"tags":[]`), []byte(`"tags":[null]`), 1)
	if _, e := ParseMetadata(bad); e == nil {
		t.Fatal("null tag accepted")
	}
	alias := bytes.Replace(base, []byte(`"size":0`), []byte(`"size":0,"Size":1`), 1)
	m, e := ParseMetadata(alias)
	if e != nil || m.Size != 0 {
		t.Fatal("case alias altered known field")
	}
	m.Name = string([]byte{255})
	if _, e := MarshalMetadata(m); e == nil {
		t.Fatal("invalid Unicode silently replaced")
	}
	paired := bytes.Replace(base, []byte(`fixture`), []byte(`\ud83d\udd10`), 1)
	if _, e := ParseMetadata(paired); e != nil {
		t.Fatal("valid surrogate pair rejected")
	}
}
