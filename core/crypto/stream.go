package crypto

import (
	"encoding/binary"
	"io"
)

func ChunkCount(size uint64) uint64 {
	if size == 0 {
		return 1
	}
	return (size-1)/ChunkSize + 1
}
func write(w io.Writer, b []byte) error {
	n, e := w.Write(b)
	if e != nil || n != len(b) {
		return ErrIO
	}
	return nil
}
func end(r io.Reader) error {
	var b [1]byte
	n, e := io.ReadFull(r, b[:])
	if n != 0 || e != io.EOF {
		return ErrInvalid
	}
	return nil
}

// EncryptStream requires a fresh file key for each content revision. The caller
// must discard partial output on any error, including changes to source length.
func EncryptStream(k Key, a, f ID, size uint64, r io.Reader, w io.Writer) error {
	defer Wipe(k[:])
	if size > MaxFileSize || a == (ID{}) || f == (ID{}) {
		return ErrInvalid
	}
	buf := make([]byte, ChunkSize)
	defer Wipe(buf)
	left := size
	count := ChunkCount(size)
	for i := uint64(0); i < count; i++ {
		n := min(uint64(ChunkSize), left)
		p := buf[:int(n)]
		if _, e := io.ReadFull(r, p); e != nil {
			return ErrIO
		}
		b, e := Seal(k, Context{Kind: Chunk, Account: a, Object: f, Index: i, Final: i == count-1}, p, nil)
		if e != nil {
			return e
		}
		if e = write(w, b); e != nil {
			return e
		}
		left -= n
	}
	return end(r)
}

// DecryptStream emits individually authenticated chunks. Output is provisional
// until this function succeeds: use private staging, not a public destination.
func DecryptStream(k Key, a, f ID, size uint64, r io.Reader, w io.Writer) error {
	defer Wipe(k[:])
	if size > MaxFileSize || a == (ID{}) || f == (ID{}) {
		return ErrInvalid
	}
	count := ChunkCount(size)
	left := size
	header := make([]byte, 71)
	for i := uint64(0); i < count; i++ {
		if _, e := io.ReadFull(r, header); e != nil {
			return ErrInvalid
		}
		n := uint64(binary.BigEndian.Uint32(header[43:47]))
		expected := min(uint64(ChunkSize), left)
		if n != expected {
			return ErrInvalid
		}
		b := make([]byte, 71+int(n)+16)
		copy(b, header)
		if _, e := io.ReadFull(r, b[71:]); e != nil {
			return ErrInvalid
		}
		p, e := Open(k, Context{Kind: Chunk, Account: a, Object: f, Index: i, Final: i == count-1}, b, nil)
		if e != nil {
			return e
		}
		e = write(w, p)
		Wipe(p)
		if e != nil {
			return e
		}
		left -= n
	}
	return end(r)
}
