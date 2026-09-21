// Package syncwire defines the M3 transport, not a new cryptographic format.
package syncwire

const PageSize = 16
const MaxPageBytes = 24 * 1024 * 1024

type Revision struct {
	ID       string `json:"id"`
	Object   string `json:"object"`
	Base     string `json:"base"`
	Metadata []byte `json:"metadata"`
	FileKey  []byte `json:"file_key,omitempty"`
}
type Change struct {
	Sequence int64    `json:"sequence"`
	Revision Revision `json:"revision"`
}
type Page struct {
	Changes []Change `json:"changes"`
}
type Cursor struct {
	Sequence int64 `json:"sequence"`
}
