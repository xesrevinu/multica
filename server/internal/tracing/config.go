package tracing

import (
	"os"
	"strings"
)

const defaultServiceName = "multica-api"

// Config is the process-level tracing switch. An empty Endpoint leaves the
// SDK uninstalled so production stays a no-op unless an operator opts in.
type Config struct {
	Endpoint    string
	ServiceName string
}

func ConfigFromEnv() Config {
	name := strings.TrimSpace(os.Getenv("MULTICA_OTEL_SERVICE_NAME"))
	if name == "" {
		name = defaultServiceName
	}
	return Config{
		Endpoint:    strings.TrimSpace(os.Getenv("MULTICA_OTEL_ENDPOINT")),
		ServiceName: name,
	}
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.Endpoint) != ""
}
