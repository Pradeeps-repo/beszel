package common

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel/internal/entities/smart"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/henrygd/beszel/internal/entities/systemd"
)

type WebSocketAction = uint8

const (
	// Request system data from agent
	GetData WebSocketAction = iota
	// Check the fingerprint of the agent
	CheckFingerprint
	// Request container logs from agent
	GetContainerLogs
	// Request container info from agent
	GetContainerInfo
	// Request SMART data from agent
	GetSmartData
	// Request detailed systemd service info from agent
	GetSystemdInfo
	// Request ZFS detail data from agent
	GetZfsData
	// Sync network monitor configuration to agent
	SyncNetworkMonitors
	// Request the list of pending package updates from agent
	GetPackageUpdates
	// Request recent logs for a systemd service from the agent.
	GetSystemdLogs
	// Ask the agent to install the configured release build.
	// The hub sends a version, not a URL or a command.
	ApplyAgentUpdate
	// Add new actions here...
)

// HubRequest defines the structure for requests sent from hub to agent.
type HubRequest[T any] struct {
	Action WebSocketAction `cbor:"0,keyasint"`
	Data   T               `cbor:"1,keyasint,omitempty,omitzero"`
	Id     *uint32         `cbor:"2,keyasint,omitempty"`
}

// AgentResponse defines the structure for responses sent from agent to hub.
type AgentResponse struct {
	Id          *uint32                    `cbor:"0,keyasint,omitempty"`
	SystemData  *system.CombinedData       `cbor:"1,keyasint,omitempty,omitzero"` // Legacy (<= 0.17)
	Fingerprint *FingerprintResponse       `cbor:"2,keyasint,omitempty,omitzero"` // Legacy (<= 0.17)
	Error       string                     `cbor:"3,keyasint,omitempty,omitzero"`
	String      *string                    `cbor:"4,keyasint,omitempty,omitzero"` // Legacy (<= 0.17)
	SmartData   map[string]smart.SmartData `cbor:"5,keyasint,omitzero"`           // Legacy (<= 0.17)
	ServiceInfo systemd.ServiceDetails     `cbor:"6,keyasint,omitempty,omitzero"` // Legacy (<= 0.17)
	// Data is the generic response payload for new endpoints (0.18+)
	Data          cbor.RawMessage `cbor:"7,keyasint,omitempty,omitzero"`
	SmartComplete bool            `cbor:"8,keyasint,omitempty,omitzero"`
}

type FingerprintRequest struct {
	Signature   []byte `cbor:"0,keyasint"`
	NeedSysInfo bool   `cbor:"1,keyasint"` // For universal token system creation
}

type FingerprintResponse struct {
	Fingerprint string `cbor:"0,keyasint"`
	// Optional system info for universal token system creation
	Hostname string `cbor:"1,keyasint,omitzero"`
	Port     string `cbor:"2,keyasint,omitzero"`
	Name     string `cbor:"3,keyasint,omitzero"`
}

type DataRequestOptions struct {
	CacheTimeMs    uint16 `cbor:"0,keyasint"`
	IncludeDetails bool   `cbor:"1,keyasint"`
}

type ZfsDataRequest struct {
	Force bool `cbor:"0,keyasint,omitempty"`
}

type ContainerLogsRequest struct {
	ContainerID string `cbor:"0,keyasint"`
}

type ContainerInfoRequest struct {
	ContainerID string `cbor:"0,keyasint"`
}

type SystemdInfoRequest struct {
	ServiceName string `cbor:"0,keyasint"`
}

type SystemdLogsRequest struct {
	ServiceName string `cbor:"0,keyasint"`
}

// AgentUpdateRequest is the only payload the hub may send to start an update.
// The agent downloads from its configured release and ignores any other instruction.
type AgentUpdateRequest struct {
	Version string `cbor:"0,keyasint"`
	Commit  string `cbor:"1,keyasint,omitempty"`
}

// AgentUpdateResponse is returned before the agent restarts.
type AgentUpdateResponse struct {
	Accepted bool   `cbor:"0,keyasint"`
	Status   string `cbor:"1,keyasint"`
	Error    string `cbor:"2,keyasint,omitempty"`
	Version  string `cbor:"3,keyasint,omitempty"`
	Commit   string `cbor:"4,keyasint,omitempty"`
}
