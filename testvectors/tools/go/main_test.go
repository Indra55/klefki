package main

import "testing"

func TestCommittedVectors(t *testing.T) {
	if err := verify("../../vectors-v1.json"); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryRejectsInvalid(t *testing.T) {
	for _, s := range []string{"", "vlt1-", "VLT1-AAAA", recoveryCode(seq(32, 64)) + "A"} {
		if _, err := recoveryDecode(s); err == nil {
			t.Fatal("invalid recovery code accepted")
		}
	}
}

// Exercise the recovery path through actual decryptions, not just equal fixture fields.
func TestRecoveryUnlocksDocument(t *testing.T) {
	v := build()
	find := func(id string) Record {
		for _, r := range v.Records {
			if r.ID == id {
				return r
			}
		}
		t.Fatal("missing fixture")
		return Record{}
	}
	key, err := recoveryDecode(v.RecoveryCode)
	if err != nil {
		t.Fatal("recovery decode failed")
	}
	r := find("master-recovery")
	r.Key = hx(key)
	mk, err := open(unhex(r.Encoded), r)
	if err != nil {
		t.Fatal("recovery unwrap failed")
	}
	r = find("file-key")
	r.Key = hx(mk)
	fk, err := open(unhex(r.Encoded), r)
	if err != nil {
		t.Fatal("file key unwrap failed")
	}
	r = find("chunk-binary")
	r.Key = hx(fk)
	p, err := open(unhex(r.Encoded), r)
	if err != nil || hx(p) != r.Plain {
		t.Fatal("recovered document mismatch")
	}
	r = find("master-recovery")
	key[0] ^= 1
	r.Key = hx(key)
	if _, err := open(unhex(r.Encoded), r); err == nil {
		t.Fatal("wrong recovery key accepted")
	}
}
