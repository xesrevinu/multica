//go:build windows

package daemon

import "github.com/multica-ai/multica/server/pkg/protocol"

func startPTY(protocol.PTYControl) (*ptySession, error) {
	return nil, errPTYUnsupported
}
