package lark

import (
	"context"
	"database/sql"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dbAuditLogger is the concrete AuditLogger implementation backed by
// lark_inbound_audit. It deliberately holds no caches and no
// indirection — the RecordLarkInboundDrop query rejects any column
// that could carry a message body, and this struct does too.
type dbAuditLogger struct {
	queries *ChannelStore
}

// NewAuditLogger returns an AuditLogger that writes drop events to the
// channel_inbound_audit table. The interface signature does not accept
// a message body, mirroring §4.7 of the design (non-content audit only).
func NewAuditLogger(queries *db.Queries) AuditLogger {
	return &dbAuditLogger{queries: NewChannelStore(queries)}
}

func (l *dbAuditLogger) RecordDrop(ctx context.Context, p AuditDropParams) error {
	return l.queries.RecordLarkInboundDrop(ctx, RecordInboundDropParams{
		EventType:        p.EventType,
		DropReason:       string(p.Reason),
		InstallationID:   p.InstallationID,
		ChannelChatID:    textIfNonEmpty(string(p.ChatID)),
		ChannelEventID:   textIfNonEmpty(p.LarkEventID),
		ChannelMessageID: textIfNonEmpty(p.LarkMessageID),
	})
}

// textIfNonEmpty returns a Valid sql.NullString for non-empty strings and
// a NULL sql.NullString otherwise. Avoids storing literal empty strings
// in the audit table, which would mask the difference between "the
// event lacked this field" and "the field was deliberately empty".
func textIfNonEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
