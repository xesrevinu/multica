package lark

import (
	"database/sql"
	"time"
)

// Domain parameter types for the channel-backed Feishu store. They replace the
// retired db.*LarkParams shapes generated from queries/lark.sql, using the same
// channel-neutral field names as the domain entities in store.go. The store
// (channel_store.go) maps them onto the channel_* writes, folding the
// feishu-specific identifiers into the JSONB config at the DB boundary.

// GetInstallationInWorkspaceParams scopes an installation lookup to a workspace.
type GetInstallationInWorkspaceParams struct {
	ID          string
	WorkspaceID string
}

// UpsertInstallationParams carries the flat feishu installation fields for an
// install / re-install.
type UpsertInstallationParams struct {
	WorkspaceID        string
	AgentID            string
	AppID              string
	AppSecretEncrypted []byte
	BotOpenID          string
	InstallerUserID    string
	TenantKey          sql.NullString
	BotUnionID         sql.NullString
	Region             string
}

// SetInstallationStatusParams flips an installation's status (active/revoked).
type SetInstallationStatusParams struct {
	ID     string
	Status string
}

// SetInstallationBotUnionIDParams records the bot's union_id (backfill).
type SetInstallationBotUnionIDParams struct {
	ID         string
	BotUnionID sql.NullString
}

// AcquireWSLeaseParams fences the WS supervisor lease for an installation.
type AcquireWSLeaseParams struct {
	NewToken     sql.NullString
	NewExpiresAt sql.NullString
	ID           string
}

// ReleaseWSLeaseParams releases a WS supervisor lease the caller still holds.
type ReleaseWSLeaseParams struct {
	ID           string
	CurrentToken sql.NullString
}

// GetUserBindingByOpenIDParams looks up a binding by its channel-native user id.
type GetUserBindingByOpenIDParams struct {
	InstallationID string
	ChannelUserID  string
}

// CreateUserBindingParams binds a workspace member to a channel-native user id.
type CreateUserBindingParams struct {
	WorkspaceID    string
	MulticaUserID  string
	InstallationID string
	ChannelUserID  string
	UnionID        sql.NullString
}

// GetChatSessionBindingParams looks up a chat binding by its channel chat id.
type GetChatSessionBindingParams struct {
	InstallationID string
	ChannelChatID  string
}

// UpdateChatSessionBindingReplyTargetParams records the latest inbound trigger
// message + thread so the outbound patcher can thread its reply.
type UpdateChatSessionBindingReplyTargetParams struct {
	ChatSessionID string
	LastMessageID sql.NullString
	LastThreadID  sql.NullString
}

// ClaimInboundDedupParams claims the two-phase idempotency row for a message.
type ClaimInboundDedupParams struct {
	InstallationID string
	MessageID      string
}

// MarkInboundDedupProcessedParams marks a claimed message processed (fenced).
type MarkInboundDedupProcessedParams struct {
	InstallationID string
	MessageID      string
	ClaimToken     string
}

// ReleaseInboundDedupParams releases a claim on processing failure (fenced).
type ReleaseInboundDedupParams struct {
	InstallationID string
	MessageID      string
	ClaimToken     string
}

// RecordInboundDropParams writes a non-content drop audit row.
type RecordInboundDropParams struct {
	EventType        string
	DropReason       string
	InstallationID   string
	ChannelChatID    sql.NullString
	ChannelEventID   sql.NullString
	ChannelMessageID sql.NullString
}

// CreateBindingTokenParams mints a short-lived channel binding token.
type CreateBindingTokenParams struct {
	TokenHash      string
	WorkspaceID    string
	InstallationID string
	ChannelUserID  string
	ExpiresAt      time.Time
}

// CreateOutboundCardMessageParams records an outbound card for a task/session.
type CreateOutboundCardMessageParams struct {
	ChatSessionID        string
	ChannelChatID        string
	ChannelCardMessageID string
	Status               string
	TaskID               string
}

// UpdateOutboundCardStatusParams transitions an outbound card's status.
type UpdateOutboundCardStatusParams struct {
	ID     string
	Status string
}
