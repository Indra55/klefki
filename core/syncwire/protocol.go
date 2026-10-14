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
	Mode              string `json:"mode"`
	Epoch             string `json:"epoch"`
	Queued            int    `json:"queued"`
	DeletionSupported bool   `json:"deletion_supported,omitempty"`
	DeletionSequence  int64  `json:"deletion_sequence,omitempty"`
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

const MaxDeviceStateVersion int64 = 9007199254740991

type DeviceState struct {
	Version    int64 `json:"version"`
	Pending    bool  `json:"pending"`
	ReportedAt int64 `json:"reportedAt"`
}

type DeviceStateUpdate struct {
	Device  string `json:"device"`
	Version int64  `json:"version"`
	Pending bool   `json:"pending"`
}

type DeviceStatus struct {
	Presence
	DeviceState *DeviceState `json:"deviceState,omitempty"`
	LastSeen    int64        `json:"lastSeen"`
	Sequence    int64        `json:"sequence"`
	Outstanding int          `json:"outstanding"`
}

type Status struct {
	DeviceStateSupported bool           `json:"deviceStateSupported,omitempty"`
	Epoch                string         `json:"epoch"`
	LatestSequence       int64          `json:"latestSequence"`
	DeletionSequence     int64          `json:"deletionSequence,omitempty"`
	Devices              []DeviceStatus `json:"devices"`
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

const RelayBatchSize = 256

type RelayPrepare struct {
	RelayDevice
	IDs []string `json:"ids"`
}
type RelayPrepared struct {
	RelayState
	IDs []string `json:"ids"`
}

// Deletion is a permanent object fence; it contains no document plaintext.
type Deletion struct {
	Object string   `json:"object"`
	Folder bool     `json:"folder"`
	Files  []string `json:"files"`
	Heads  []string `json:"heads,omitempty"`
}
type DeletionBatch struct {
	Deletions []Deletion `json:"deletions"`
}
type DeletionChange struct {
	Sequence int64    `json:"sequence"`
	Deletion Deletion `json:"deletion"`
}
type DeletionPage struct {
	Epoch   string           `json:"epoch"`
	Latest  int64            `json:"latest"`
	Enabled bool             `json:"enabled"`
	Changes []DeletionChange `json:"changes"`
}
