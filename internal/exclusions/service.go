package exclusions

import (
	"context"
	"fmt"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
)

// AddResult is returned after adding an excluded sender.
type AddResult struct {
	Sender                 domain.ExcludedSender
	AlreadyExcluded        bool
	HiddenExistingMessages int64
}

// Service coordinates exclusion policy with stored messages.
type Service struct {
	exclusions *Repository
	messages   *messages.Repository
}

// NewService creates an exclusion service.
func NewService(exclusions *Repository, messages *messages.Repository) *Service {
	return &Service{exclusions: exclusions, messages: messages}
}

// IsExcluded reports whether sender is excluded globally or for sourceID.
func (s *Service) IsExcluded(ctx context.Context, sender domain.Sender, sourceID string) (bool, error) {
	if s == nil || s.exclusions == nil {
		return false, fmt.Errorf("exclusion service is required")
	}
	_, ok, err := s.exclusions.FindMatching(ctx, sender, domain.ExclusionScopeGlobal, "")
	if err != nil || ok {
		return ok, err
	}
	if sourceID == "" {
		return false, nil
	}
	_, ok, err = s.exclusions.FindMatching(ctx, sender, domain.ExclusionScopeSource, sourceID)
	return ok, err
}

// AddSender excludes a sender and hides matching stored messages.
func (s *Service) AddSender(ctx context.Context, params AddSenderParams) (AddResult, error) {
	if s == nil || s.exclusions == nil || s.messages == nil {
		return AddResult{}, fmt.Errorf("exclusion service is required")
	}
	params.Scope = normalizeScope(params.Scope)

	entry, already, err := s.exclusions.Add(ctx, params)
	if err != nil {
		return AddResult{}, err
	}

	hidden, err := s.messages.HideBySender(ctx, params.Sender, params.Scope, params.SourceID)
	if err != nil {
		return AddResult{}, err
	}

	return AddResult{Sender: entry, AlreadyExcluded: already, HiddenExistingMessages: hidden}, nil
}

// AddFromMessage excludes a stored message author and stores that message as evidence.
func (s *Service) AddFromMessage(ctx context.Context, messageExternalID string, reason string, createdBy string, scope domain.ExclusionScope, sourceID string) (AddResult, error) {
	if s == nil || s.messages == nil {
		return AddResult{}, fmt.Errorf("exclusion service is required")
	}
	message, ok, err := s.messages.Get(ctx, messageExternalID)
	if err != nil {
		return AddResult{}, err
	}
	if !ok {
		return AddResult{}, fmt.Errorf("message %q not found", messageExternalID)
	}

	if scope == "" {
		scope = domain.ExclusionScopeGlobal
	}
	if scope == domain.ExclusionScopeSource && sourceID == "" {
		sourceID = message.SourceID
	}

	return s.AddSender(ctx, AddSenderParams{
		Sender: message.Sender,
		Reason: reason,
		Evidence: domain.EvidenceMessage{
			ExternalID:  message.ExternalID,
			Text:        message.Text,
			Link:        message.Link,
			Date:        message.Date,
			SourceID:    message.SourceID,
			SourceTitle: message.ChatTitle,
		},
		Scope:     scope,
		SourceID:  sourceID,
		CreatedBy: createdBy,
	})
}

// RemoveSender removes a sender from the exclusion list.
func (s *Service) RemoveSender(ctx context.Context, sender domain.Sender, scope domain.ExclusionScope, sourceID string) (bool, error) {
	if s == nil || s.exclusions == nil {
		return false, fmt.Errorf("exclusion service is required")
	}
	return s.exclusions.RemoveMatching(ctx, sender, scope, sourceID)
}
