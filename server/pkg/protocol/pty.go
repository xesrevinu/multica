package protocol

import "github.com/google/uuid"

// PTY control-plane types for the dedicated binary WebSocket (FX-139).
// Data-plane bytes travel as BinaryMessage and are never wrapped in JSON.

const (
	PTYTypeOpen   = "pty.open"
	PTYTypeOpened = "pty.opened"
	PTYTypeResize = "pty.resize"
	PTYTypeClose  = "pty.close"
	PTYTypeExit   = "pty.exit"
	PTYTypeError  = "pty.error"

	// PTYFrameIDSize is the UUID prefix on daemon-facing binary frames so
	// multiple PTY sessions can share one daemon websocket without mixing bytes.
	PTYFrameIDSize = 16
)

// PTYControl is the small JSON envelope for open/resize/close/exit/error.
// Binary frames on the same socket carry raw PTY bytes in both directions.
type PTYControl struct {
	Type  string   `json:"type"`
	ID    string   `json:"id,omitempty"`
	Cols  uint16   `json:"cols,omitempty"`
	Rows  uint16   `json:"rows,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
	Argv  []string `json:"argv,omitempty"`
	PID   int      `json:"pid,omitempty"`
	Code  *int     `json:"code,omitempty"`
	Error string   `json:"error,omitempty"`
}

// ParsePTYID maps a pane/session id onto a stable 16-byte UUID.
func ParsePTYID(id string) uuid.UUID {
	if id == "" {
		return uuid.Nil
	}
	if parsed, err := uuid.Parse(id); err == nil {
		return parsed
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(id))
}

func EncodePTYFrame(id uuid.UUID, payload []byte) []byte {
	out := make([]byte, PTYFrameIDSize+len(payload))
	copy(out, id[:])
	copy(out[PTYFrameIDSize:], payload)
	return out
}

func DecodePTYFrame(frame []byte) (uuid.UUID, []byte, bool) {
	if len(frame) < PTYFrameIDSize {
		return uuid.Nil, nil, false
	}
	var id uuid.UUID
	copy(id[:], frame[:PTYFrameIDSize])
	return id, frame[PTYFrameIDSize:], true
}
