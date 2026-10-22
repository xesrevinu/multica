package realtime

import "encoding/json"

// envelope is the in-process shape previously carried on the Redis relay.
// Membership removal still delivers these frames through the local hub.
type envelope struct {
	EventID     string
	EventType   string
	Scope       string
	ScopeID     string
	WorkspaceID string
	ActorID     string
	CreatedAt   string
	NodeID      string
	PayloadJSON string
}

func deliverEnvelope(hub *Hub, daemonRuntime DaemonRuntimeDeliverer, ev envelope) {
	if ev.PayloadJSON == "" || hub == nil {
		return
	}
	frame := injectEventID([]byte(ev.PayloadJSON), ev.EventID)
	switch ev.Scope {
	case ScopeDaemonRuntime:
		if daemonRuntime != nil {
			daemonRuntime.DeliverDaemonRuntime(ev.ScopeID, frame, ev.EventID)
		}
	case "global":
		hub.fanoutAllDedup(frame, "", ev.EventID)
	case ScopeUser:
		hub.fanoutUser(ev.ScopeID, frame, ev.WorkspaceID, ev.EventID)
	default:
		hub.BroadcastToScopeDedup(ev.Scope, ev.ScopeID, frame, ev.EventID)
	}
}

// injectEventID inserts event_id into a JSON object frame. Non-object frames
// are returned unchanged.
func injectEventID(frame []byte, eventID string) []byte {
	if eventID == "" || len(frame) == 0 || frame[0] != '{' {
		return frame
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(frame, &obj); err != nil {
		return frame
	}
	if _, exists := obj["event_id"]; exists {
		return frame
	}
	idJSON, _ := json.Marshal(eventID)
	obj["event_id"] = idJSON
	out, err := json.Marshal(obj)
	if err != nil {
		return frame
	}
	return out
}
