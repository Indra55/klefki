package crypto

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestFolderIconMetadata(t *testing.T) {
	data, e := os.ReadFile("../../testvectors/folder-icons.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases struct {
		Valid   []string
		Invalid []json.RawMessage
	}
	if e = json.Unmarshal(data, &cases); e != nil {
		t.Fatal(e)
	}
	for _, icon := range cases.Valid {
		m := Metadata{Type: "folder", Name: "public folder", Tags: []string{}, FolderIcon: icon}
		data, e := EncryptMetadata(Key{}, ID{1}, ID{2}, m)
		if e != nil {
			t.Fatalf("valid icon %q: %v", icon, e)
		}
		got, e := DecryptMetadata(Key{}, ID{1}, ID{2}, data)
		if e != nil || got.FolderIcon != icon {
			t.Fatalf("icon lost: %q %v", icon, e)
		}
		if icon != "" && bytes.Contains(data, []byte(icon)) {
			t.Fatal("plaintext icon leaked")
		}
		plain, e := MarshalMetadata(m)
		if e != nil || icon == "" && bytes.Contains(plain, []byte("folder_icon")) {
			t.Fatal("default icon must be omitted", e)
		}
	}
	for _, icon := range cases.Invalid {
		raw := []byte(`{"type":"folder","name":"public folder","parent":null,"tags":[],"favorite":false,"folder_icon":` + string(icon) + `}`)
		if _, e := ParseMetadata(raw); e == nil {
			t.Fatalf("invalid icon accepted: %s", icon)
		}
	}
	file := []byte(`{"type":"file","name":"public.txt","parent":null,"tags":[],"favorite":false,"file_id":"1112131415161718191a1b1c1d1e1f20","size":0,"chunk_count":1,"folder_icon":""}`)
	if _, e := ParseMetadata(file); e == nil {
		t.Fatal("file icon accepted")
	}
	if _, e := MarshalMetadata(Metadata{Type: "folder", Name: "public folder", Tags: []string{}, FolderIcon: "../invalid"}); e == nil {
		t.Fatal("invalid icon serialized")
	}
}
