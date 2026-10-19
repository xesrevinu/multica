package execenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
)

// prepareYieldedMcpConfig writes the agent's merged mcp_config to
// {workDir}/.mcp.json. yielded-agent reads that file at the start of a turn.
// A null config leaves the file alone. Any other object is a managed set.
func prepareYieldedMcpConfig(workDir, provider string, raw json.RawMessage, manifest *sidecarManifest) error {
	if provider != "yielded" {
		return nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if workDir == "" {
		return errors.New("managed mcp_config requires a working directory")
	}
	if !json.Valid(trimmed) {
		return errors.New("yielded mcp_config is not valid JSON")
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &document); err != nil {
		return errors.New("yielded mcp_config must be a JSON object")
	}
	if servers, ok := document["mcpServers"]; ok {
		var names map[string]json.RawMessage
		if err := json.Unmarshal(servers, &names); err != nil {
			return errors.New("yielded mcp_config mcpServers must be an object")
		}
	}
	path := filepath.Join(workDir, ".mcp.json")
	if err := recordWriteFile(path, append(append([]byte{}, trimmed...), '\n'), 0o600, manifest); err != nil {
		if errors.Is(err, errPathPreExists) {
			return errors.New("managed mcp_config would overwrite existing " + path)
		}
		return err
	}
	return nil
}
