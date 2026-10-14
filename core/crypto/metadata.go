package crypto

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Metadata struct {
	Type                string   `json:"type"`
	Name                string   `json:"name"`
	Parent              *string  `json:"parent"`
	Tags                []string `json:"tags"`
	Favorite            bool     `json:"favorite"`
	FileID              string   `json:"file_id,omitempty"`
	Size                uint64   `json:"size"`
	ChunkCount          uint64   `json:"chunk_count"`
	TrashedAt           int64    `json:"trashed_at,omitempty"`
	TrashPreviousParent *string  `json:"trash_previous_parent,omitempty"`
	FolderIcon          string   `json:"folder_icon,omitempty"`
}

var folderIconPattern = regexp.MustCompile(`^[0-9a-f]{4,6}(-[0-9a-f]{4,6}){0,7}$`)

func ValidFolderIcon(icon string) bool {
	if icon == "" {
		return true
	}
	if len(icon) > 55 || !folderIconPattern.MatchString(icon) {
		return false
	}
	for _, part := range strings.Split(icon, "-") {
		value, e := strconv.ParseUint(part, 16, 32)
		if e != nil || value == 0 || value > 0x10ffff || value >= 0xd800 && value <= 0xdfff {
			return false
		}
	}
	return true
}

func ParseID(s string) (out ID, e error) {
	if len(s) != 32 {
		return out, ErrInvalid
	}
	b, e := hex.DecodeString(s)
	if e != nil || hex.EncodeToString(b) != s {
		return out, ErrInvalid
	}
	copy(out[:], b)
	if out == (ID{}) {
		return out, ErrInvalid
	}
	return out, nil
}
func (id ID) String() string { return hex.EncodeToString(id[:]) }

// validUnicodeEscapes prevents encoding/json from silently replacing lone UTF-16
// surrogates. JSON syntax itself is validated separately by the standard decoder.
func validUnicodeEscapes(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return false
		}
		n, e := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if n >= 0xD800 && n <= 0xDBFF {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			low, e := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
			if e != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		} else if n >= 0xDC00 && n <= 0xDFFF {
			return false
		}
	}
	return true
}
func jsonValue(d *json.Decoder, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	tok, e := d.Token()
	if e != nil {
		return ErrInvalid
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			tok, e := d.Token()
			s, ok := tok.(string)
			if e != nil || !ok || seen[s] {
				return ErrInvalid
			}
			seen[s] = true
			if jsonValue(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		tok, e = d.Token()
		if e != nil || tok != json.Delim('}') {
			return ErrInvalid
		}
	case '[':
		for d.More() {
			if jsonValue(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		tok, e = d.Token()
		if e != nil || tok != json.Delim(']') {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func ParseMetadata(b []byte) (m Metadata, e error) {
	if len(b) > MaxMetadata || !utf8.Valid(b) || !validUnicodeEscapes(b) {
		return m, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if jsonValue(d, 0) != nil {
		return m, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return m, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil || fields == nil {
		return m, ErrInvalid
	}
	for _, name := range []string{"type", "name", "parent", "tags", "favorite"} {
		raw, ok := fields[name]
		if !ok || (name != "parent" && bytes.Equal(raw, []byte("null"))) {
			return m, ErrInvalid
		}
	}
	// Decode exact field names: encoding/json struct matching also accepts case
	// aliases, which must not override the spec's authenticated known fields.
	for name, target := range map[string]any{"type": &m.Type, "name": &m.Name,
		"parent": &m.Parent, "tags": &m.Tags, "favorite": &m.Favorite} {
		if json.Unmarshal(fields[name], target) != nil {
			return m, ErrInvalid
		}
	}
	if (m.Type != "file" && m.Type != "folder") || m.Tags == nil {
		return m, ErrInvalid
	}
	var tags []json.RawMessage
	if json.Unmarshal(fields["tags"], &tags) != nil {
		return m, ErrInvalid
	}
	for _, tag := range tags {
		if bytes.Equal(bytes.TrimSpace(tag), []byte("null")) {
			return m, ErrInvalid
		}
	}
	if m.Parent != nil {
		if _, e = ParseID(*m.Parent); e != nil {
			return m, ErrInvalid
		}
	}
	if m.Type == "file" {
		for _, name := range []string{"file_id", "size", "chunk_count"} {
			raw, ok := fields[name]
			if !ok || bytes.Equal(raw, []byte("null")) {
				return m, ErrInvalid
			}
		}
		for name, target := range map[string]any{"file_id": &m.FileID, "size": &m.Size, "chunk_count": &m.ChunkCount} {
			if json.Unmarshal(fields[name], target) != nil {
				return m, ErrInvalid
			}
		}
		if _, e = ParseID(m.FileID); e != nil || m.Size > MaxFileSize || m.ChunkCount != ChunkCount(m.Size) {
			return m, ErrInvalid
		}
	} else {
		for _, name := range []string{"file_id", "size", "chunk_count"} {
			if _, ok := fields[name]; ok {
				return m, ErrInvalid
			}
		}
	}
	for name, target := range map[string]any{"trashed_at": &m.TrashedAt, "trash_previous_parent": &m.TrashPreviousParent} {
		if raw, ok := fields[name]; ok && (bytes.Equal(raw, []byte("null")) && name == "trashed_at" || json.Unmarshal(raw, target) != nil) {
			return m, ErrInvalid
		}
	}
	if m.TrashedAt < 0 || m.TrashedAt > 9007199254740991 {
		return m, ErrInvalid
	}
	if m.TrashPreviousParent != nil {
		if _, e = ParseID(*m.TrashPreviousParent); e != nil {
			return m, e
		}
	}
	if raw, ok := fields["folder_icon"]; ok {
		if m.Type != "folder" || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &m.FolderIcon) != nil || !ValidFolderIcon(m.FolderIcon) {
			return m, ErrInvalid
		}
	}
	// Unknown fields remain encrypted and are ignored for forward compatibility.
	return m, nil
}
func MarshalMetadata(m Metadata) ([]byte, error) {
	if !utf8.ValidString(m.Type) || !utf8.ValidString(m.Name) || !utf8.ValidString(m.FileID) || (m.Parent != nil && !utf8.ValidString(*m.Parent)) {
		return nil, ErrInvalid
	}
	for _, tag := range m.Tags {
		if !utf8.ValidString(tag) {
			return nil, ErrInvalid
		}
	}
	var b []byte
	var e error
	if m.Type == "folder" {
		b, e = json.Marshal(struct {
			Type                string   `json:"type"`
			Name                string   `json:"name"`
			Parent              *string  `json:"parent"`
			Tags                []string `json:"tags"`
			Favorite            bool     `json:"favorite"`
			TrashedAt           int64    `json:"trashed_at,omitempty"`
			TrashPreviousParent *string  `json:"trash_previous_parent,omitempty"`
			FolderIcon          string   `json:"folder_icon,omitempty"`
		}{m.Type, m.Name, m.Parent, m.Tags, m.Favorite, m.TrashedAt, m.TrashPreviousParent, m.FolderIcon})
	} else {
		b, e = json.Marshal(m)
	}
	if e != nil {
		return nil, ErrInvalid
	}
	if _, e = ParseMetadata(b); e != nil {
		Wipe(b)
		return nil, e
	}
	return b, nil
}
func EncryptMetadata(k Key, a, o ID, m Metadata) ([]byte, error) {
	defer Wipe(k[:])
	p, e := MarshalMetadata(m)
	if e != nil {
		return nil, e
	}
	defer Wipe(p)
	return Seal(k, Context{Kind: MetadataRecord, Account: a, Object: o}, p, nil)
}
func DecryptMetadata(k Key, a, o ID, b []byte) (Metadata, error) {
	defer Wipe(k[:])
	p, e := Open(k, Context{Kind: MetadataRecord, Account: a, Object: o}, b, nil)
	if e != nil {
		return Metadata{}, e
	}
	defer Wipe(p)
	return ParseMetadata(p)
}
