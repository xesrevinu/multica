package lark

import (
	"github.com/multica-ai/multica/server/internal/util"
)

// uuidString renders a string as its canonical string form. Shared by
// the package's logging and map-keying sites. (Formerly defined on hub.go,
// which the channel-engine cutover removed.)
func uuidString(u string) string { return util.UUIDToString(u) }
