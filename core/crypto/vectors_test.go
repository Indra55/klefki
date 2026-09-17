package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

var vectorAccount = ID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
var vectorFile = ID{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}

func sequentialKey(start byte) (k Key) {
	for i := range k {
		k[i] = start + byte(i)
	}
	return
}
func vectorStream(t *testing.T, n int) [][]byte {
	t.Helper()
	count := int(ChunkCount(uint64(n)))
	bs := make([][]byte, count)
	for i := 0; i < count; i++ {
		p := make([]byte, min(ChunkSize, n-i*ChunkSize))
		for j := range p {
			p[j] = byte((i*ChunkSize + j) % 251)
		}
		nonce := sha256.Sum256([]byte(fmt.Sprintf("vault-fixture/stream-%d-%d", n, i)))
		var e error
		bs[i], e = sealNonce(sequentialKey(96), Context{Kind: Chunk, Account: vectorAccount, Object: vectorFile, Index: uint64(i), Final: i == count-1}, p, nil, nonce[:24])
		if e != nil {
			t.Fatal("stream fixture encrypt")
		}
	}
	return bs
}
func TestSharedStreamsRecoveryAndRejections(t *testing.T) {
	v := fixtures(t)
	var streams []struct {
		Length  int      `json:"length"`
		Digests []string `json:"record_sha256"`
	}
	if json.Unmarshal(v["streams"], &streams) != nil || len(streams) != 6 {
		t.Fatal("stream fixture schema")
	}
	for _, s := range streams {
		bs := vectorStream(t, s.Length)
		if len(bs) != len(s.Digests) {
			t.Fatal("stream count")
		}
		for i, b := range bs {
			d := sha256.Sum256(b)
			if hex.EncodeToString(d[:]) != s.Digests[i] {
				t.Fatal("stream digest")
			}
		}
		var out bytes.Buffer
		if e := DecryptStream(sequentialKey(96), vectorAccount, vectorFile, uint64(s.Length), bytes.NewReader(bytes.Join(bs, nil)), &out); e != nil {
			t.Fatal(e)
		}
		for i, x := range out.Bytes() {
			if x != byte(i%251) {
				t.Fatal("stream fixture plaintext")
			}
		}
	}
	var recoveryKey, recoveryCode string
	json.Unmarshal(v["recovery_key_hex"], &recoveryKey)
	json.Unmarshal(v["recovery_code"], &recoveryCode)
	rk, e := ParseRecoveryCode(recoveryCode)
	if e != nil || rk != key(decode(recoveryKey)) || RecoveryCode(rk) != recoveryCode {
		t.Fatal("shared recovery")
	}
	var records []recordFixture
	if json.Unmarshal(v["records"], &records) != nil || len(records) != 7 {
		t.Fatal("record schema")
	}
	byID := map[string]recordFixture{}
	for _, r := range records {
		byID[r.ID] = r
	}
	var negatives []struct {
		ID        string `json:"id"`
		Base      string `json:"base"`
		Operation string `json:"operation"`
	}
	if json.Unmarshal(v["negative"], &negatives) != nil || len(negatives) != 25 {
		t.Fatal("negative schema")
	}
	for _, n := range negatives {
		t.Run(n.ID, func(t *testing.T) {
			if n.Base == "kdf-0" {
				d := decode(byID["master-password"].Extra)
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
					t.Fatal("unknown mutation")
				}
				if ValidateDescriptor(d) == nil {
					t.Fatal("KDF accepted")
				}
				return
			}
			if strings.HasPrefix(n.Base, "stream-") {
				bs := vectorStream(t, ChunkSize+1)
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
					nonce := sha256.Sum256([]byte("other public fixture nonce"))
					bs[1], _ = sealNonce(sequentialKey(128), Context{Kind: Chunk, Account: vectorAccount, Object: vectorFile, Index: 1, Final: true}, []byte{byte(ChunkSize % 251)}, nil, nonce[:24])
				default:
					t.Fatal("unknown mutation")
				}
				if DecryptStream(sequentialKey(96), vectorAccount, vectorFile, ChunkSize+1, bytes.NewReader(bytes.Join(bs, nil)), io.Discard) == nil {
					t.Fatal("stream mutation accepted")
				}
				return
			}
			r, ok := byID[n.Base]
			if !ok {
				t.Fatal("unknown fixture")
			}
			b := decode(r.Encoded)
			extra := decode(r.Extra)
			k := key(decode(r.Key))
			c := Context{Kind: r.Kind, Account: id(decode(r.Account)), Object: id(decode(r.Object)), Index: r.Index, Final: r.Final}
			switch n.Operation {
			case "flip-ciphertext":
				b[len(b)-1] ^= 1
			case "flip-nonce":
				b[47] ^= 1
			case "wrong-key":
				k = sequentialKey(200)
			case "wrong-account":
				c.Account = ID{200}
			case "wrong-object":
				c.Object = ID{200}
			case "wrong-kind":
				c.Kind = MetadataRecord
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
				extra[26] ^= 1
			default:
				t.Fatal("unknown mutation")
			}
			if _, e := Open(k, c, b, extra); e == nil {
				t.Fatal("record mutation accepted")
			}
		})
	}
}
func FuzzStreamParser(f *testing.F) {
	b, _ := sealNonce(Key{}, Context{Kind: Chunk, Account: ID{1}, Object: ID{2}, Final: true}, []byte{1, 2, 3}, nil, make([]byte, 24))
	f.Add(b)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		_ = DecryptStream(Key{}, ID{1}, ID{2}, 3, bytes.NewReader(b), io.Discard)
	})
}
