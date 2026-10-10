package main

import (
	"bytes"
	"encoding/binary"
	"io"

	v "klefki/core/crypto"
	"klefki/core/local"
)

func accountBytes(a v.Account) []byte { return local.AccountBytes(a) }
func recordHeader(b []byte, kind v.Kind, account, object v.ID) bool {
	return len(b) == 106 && b[0] == 1 && b[1] == byte(kind) && v.ID(b[2:18]) == account && v.ID(b[18:34]) == object
}
func readAccount(r io.Reader) (v.Account, error) { return local.ReadAccount(r) }

type fileHeader struct {
	Account, File, Metadata    v.ID
	Wrapped, EncryptedMetadata []byte
}

func (h fileHeader) bytes() []byte {
	b := append([]byte("VLTF\x01"), h.Account[:]...)
	b = append(b, h.File[:]...)
	b = append(b, h.Metadata[:]...)
	b = append(b, h.Wrapped...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(h.EncryptedMetadata)))
	return append(b, h.EncryptedMetadata...)
}
func readFileHeader(r io.Reader) (h fileHeader, e error) {
	b := make([]byte, 163)
	if _, e = io.ReadFull(r, b); e != nil {
		return h, v.ErrInvalid
	}
	if !bytes.Equal(b[:5], []byte("VLTF\x01")) {
		return h, v.ErrInvalid
	}
	copy(h.Account[:], b[5:21])
	copy(h.File[:], b[21:37])
	copy(h.Metadata[:], b[37:53])
	h.Wrapped = b[53:159]
	n := binary.BigEndian.Uint32(b[159:163])
	if h.Account == (v.ID{}) || h.File == (v.ID{}) || h.Metadata == (v.ID{}) || n < 74 || n > v.MaxMetadata+74 || !recordHeader(h.Wrapped, v.FileKey, h.Account, h.File) {
		return fileHeader{}, v.ErrInvalid
	}
	h.EncryptedMetadata = make([]byte, int(n))
	if _, e = io.ReadFull(r, h.EncryptedMetadata); e != nil {
		return fileHeader{}, v.ErrInvalid
	}
	return h, nil
}
