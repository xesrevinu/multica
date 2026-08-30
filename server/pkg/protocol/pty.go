package protocol

// PTY control-plane types for the dedicated binary WebSocket (FX-139).
// Data-plane bytes travel as BinaryMessage and are never wrapped in JSON.

const (
	PTYTypeOpen   = "pty.open"
	PTYTypeOpened = "pty.opened"
	PTYTypeResize = "pty.resize"
	PTYTypeClose  = "pty.close"
	PTYTypeExit   = "pty.exit"
	PTYTypeError  = "pty.error"
)

// PTYControl is the small JSON envelope for open/resize/close/exit/error.
// Binary frames on the same socket carry raw PTY bytes in both directions.
type PTYControl struct {
	Type  string   `json:"type"`
	Cols  uint16   `json:"cols,omitempty"`
	Rows  uint16   `json:"rows,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
	Argv  []string `json:"argv,omitempty"`
	PID   int      `json:"pid,omitempty"`
	Code  *int     `json:"code,omitempty"`
	Error string   `json:"error,omitempty"`
}
