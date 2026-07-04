package domain

import "time"

// ExclusionScope identifies whether a sender is excluded globally or per source.
type ExclusionScope string

const (
	// ExclusionScopeGlobal excludes a sender across all configured sources.
	ExclusionScopeGlobal ExclusionScope = "global"

	// ExclusionScopeSource excludes a sender only for one source.
	ExclusionScopeSource ExclusionScope = "source"
)

// EvidenceMessage is the message that caused a sender to be excluded.
type EvidenceMessage struct {
	ExternalID  string
	Text        string
	Link        string
	Date        time.Time
	SourceID    string
	SourceTitle string
}

// ExcludedSender is a local spam-list entry.
type ExcludedSender struct {
	ID                 int64
	SenderID           int64
	Username           string
	UsernameNormalized string
	DisplayName        string
	Reason             string
	Evidence           EvidenceMessage
	Scope              ExclusionScope
	SourceID           string
	CreatedAt          time.Time
	CreatedBy          string
}
