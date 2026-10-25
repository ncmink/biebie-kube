package main

import (
	"context"
	"errors"

	"biebie-kube/internal/domain"
)

var errNoAI = errors.New("Ask AI is unavailable")

// AIService runs local AI CLIs with scrubbed resource context.
type AIService struct{ core *Core }

func (s *AIService) ServiceName() string { return "AIService" }

func (s *AIService) ListCLIs(ctx context.Context) []domain.AICliInfo {
	if s.core.ai == nil {
		return nil
	}
	return s.core.ai.ListCLIs(ctx)
}

func (s *AIService) AISettings(ctx context.Context) (domain.AISettings, error) {
	if s.core.ai == nil {
		return domain.AISettings{}, nil
	}
	return s.core.ai.Settings(), nil
}

func (s *AIService) SetAISettings(ctx context.Context, settings domain.AISettings) error {
	if s.core.ai == nil {
		return describe(errNoAI)
	}
	return describe(s.core.ai.SetSettings(settings))
}

func (s *AIService) AcknowledgeCLI(ctx context.Context, cliID string) error {
	if s.core.ai == nil {
		return describe(errNoAI)
	}
	return describe(s.core.ai.AcknowledgeCLI(cliID))
}

func (s *AIService) AskAI(ctx context.Context, req domain.AskAIRequest) (domain.AskAISession, error) {
	if s.core.ai == nil {
		return domain.AskAISession{}, describe(errNoAI)
	}
	out, err := s.core.ai.AskAI(ctx, req)
	return out, describe(err)
}

// PreviewAskAI returns what AskAI would send for this request, without
// starting a CLI.
func (s *AIService) PreviewAskAI(ctx context.Context, req domain.AskAIRequest) (domain.AIContextPreview, error) {
	if s.core.ai == nil {
		return domain.AIContextPreview{}, describe(errNoAI)
	}
	out, err := s.core.ai.PreviewAskAI(ctx, req)
	return out, describe(err)
}

func (s *AIService) CancelAskAI(ctx context.Context, sessionID string) {
	if s.core.ai != nil {
		s.core.ai.CancelAskAI(sessionID)
	}
}
