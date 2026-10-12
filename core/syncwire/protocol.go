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

const Persistent = "persistent"
const TemporaryRelay = "temporary-relay"

type SyncSettings struct {
	Mode   string `json:"mode"`
	Epoch  string `json:"epoch"`
	Queued int    `json:"queued"`
}
type RelayDevice struct {
	Device   string   `json:"device"`
	Known    []string `json:"known"`
	Sequence int64    `json:"sequence"`
}
type RelayState struct {
	SyncSettings
	Devices []string `json:"devices"`
}
type RelayNeed struct {
	IDs []string `json:"ids"`
}

type Progress struct {
	Phase       string `json:"phase"`
	Revision    string `json:"revision,omitempty"`
	Name        string `json:"name,omitempty"`
	Direction   string `json:"direction,omitempty"`
	Source      string `json:"source,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	DoneBytes   uint64 `json:"doneBytes"`
	TotalBytes  uint64 `json:"totalBytes"`
	ReusedBytes uint64 `json:"reusedBytes"`
	Chunk       uint64 `json:"chunk"`
	Chunks      uint64 `json:"chunks"`
	Attempt     int    `json:"attempt,omitempty"`
	RetryAt     int64  `json:"retryAt,omitempty"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type Presence struct {
	Device     string `json:"device"`
	ClientKind string `json:"clientKind"`
	Phase      string `json:"phase"`
	Revision   string `json:"revision,omitempty"`
	Direction  string `json:"direction,omitempty"`
	DoneBytes  uint64 `json:"doneBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

type DeviceStatus struct {
	Presence
	LastSeen    int64 `json:"lastSeen"`
	Sequence    int64 `json:"sequence"`
	Outstanding int   `json:"outstanding"`
}

type Status struct {
	Epoch          string         `json:"epoch"`
	LatestSequence int64          `json:"latestSequence"`
	Devices        []DeviceStatus `json:"devices"`
}

type Provenance struct {
	Sequence  int64  `json:"sequence"`
	Revision  string `json:"revision"`
	Origin    string `json:"origin"`
	Publisher string `json:"publisher"`
}

type ProvenancePage struct {
	Changes []Provenance `json:"changes"`
}
