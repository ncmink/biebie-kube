package ai

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/incident"
	"biebie-kube/internal/resources"
	"biebie-kube/internal/store"
)

// Service orchestrates detection, context building, and CLI runs.
type Service struct {
	store     *store.Store
	resources *resources.Service
	incident  *incident.Service
	detector  *Detector
	runner    *Runner
}

// NewService wires Ask AI.
func NewService(st *store.Store, res *resources.Service, inc *incident.Service, emitter Emitter) *Service {
	return &Service{
		store:     st,
		resources: res,
		incident:  inc,
		detector:  NewDetector(),
		runner:    NewRunner(emitter),
	}
}

// CloseAll cancels in-flight runs.
func (s *Service) CloseAll() { s.runner.CloseAll() }

// Settings returns persisted AI preferences.
func (s *Service) Settings() domain.AISettings {
	data := s.store.Read().AI
	return domain.AISettings{
		DefaultCli:   data.DefaultCli,
		Custom:       data.Custom,
		Acknowledged: data.Acknowledged,
	}
}

// SetSettings updates AI preferences.
func (s *Service) SetSettings(settings domain.AISettings) error {
	return s.store.Update(func(d *store.Data) error {
		d.AI.DefaultCli = strings.TrimSpace(settings.DefaultCli)
		d.AI.Custom = settings.Custom
		d.AI.Acknowledged = settings.Acknowledged
		return nil
	})
}

// AcknowledgeCLI records first-use consent for one CLI id.
func (s *Service) AcknowledgeCLI(cliID string) error {
	cliID = strings.TrimSpace(cliID)
	if cliID == "" {
		return errors.New("cli id is required")
	}
	return s.store.Update(func(d *store.Data) error {
		for _, id := range d.AI.Acknowledged {
			if id == cliID {
				return nil
			}
		}
		d.AI.Acknowledged = append(d.AI.Acknowledged, cliID)
		return nil
	})
}

// ListCLIs probes built-ins and appends custom commands.
func (s *Service) ListCLIs(ctx context.Context) []domain.AICliInfo {
	settings := s.Settings()
	ack := make(map[string]bool)
	for _, id := range settings.Acknowledged {
		ack[id] = true
	}

	out := s.detector.ListBuiltIn(ctx)
	for i := range out {
		out[i].Acknowledged = ack[out[i].ID]
	}

	for _, c := range settings.Custom {
		id := "custom:" + c.ID
		path := strings.TrimSpace(c.Path)
		info := domain.AICliInfo{
			ID:           id,
			Name:         c.Label,
			Command:      path,
			RunArgs:      append([]string{}, c.Args...),
			Custom:       true,
			Acknowledged: ack[id],
		}
		if path == "" {
			info.Reason = "Custom command has no path"
			out = append(out, info)
			continue
		}
		if _, err := ExecutablePath(path); err != nil {
			info.Reason = err.Error()
			out = append(out, info)
			continue
		}
		info.Path = path
		info.Available = true
		out = append(out, info)
	}
	return out
}

// AskAI builds context and starts a background CLI run.
func (s *Service) AskAI(ctx context.Context, req domain.AskAIRequest) (domain.AskAISession, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return domain.AskAISession{}, errors.New("a question is required")
	}
	if req.ClusterID == "" {
		return domain.AskAISession{}, errors.New("cluster id is required")
	}
	cliID := strings.TrimSpace(req.CliID)
	if cliID == "" {
		cliID = s.Settings().DefaultCli
	}
	if cliID == "" {
		return domain.AskAISession{}, errors.New("choose an AI CLI in Settings")
	}

	clis := s.ListCLIs(ctx)
	var path string
	for _, c := range clis {
		if c.ID == cliID && c.Available {
			path = c.Path
			break
		}
	}
	if path == "" {
		return domain.AskAISession{}, errors.New("the selected AI CLI is not available")
	}

	builder := Builder{Resources: s.resources, Incident: s.incident}
	built, err := builder.Build(ctx, req.ClusterID, req.Ref, prompt)
	if err != nil {
		return domain.AskAISession{}, err
	}

	argv, stdin, err := ResolveInvocation(cliID, s.Settings().Custom, path, built.Body)
	if err != nil {
		return domain.AskAISession{}, err
	}

	sessionID := "ai_" + uuid.NewString()
	go func() {
		_ = s.runner.Run(context.Background(), sessionID, argv, stdin)
	}()

	return domain.AskAISession{SessionID: sessionID}, nil
}

// PreviewAskAI builds the same message AskAI would send, without running a CLI.
func (s *Service) PreviewAskAI(ctx context.Context, req domain.AskAIRequest) (domain.AIContextPreview, error) {
	if req.ClusterID == "" {
		return domain.AIContextPreview{}, errors.New("cluster id is required")
	}
	builder := Builder{Resources: s.resources, Incident: s.incident}
	built, err := builder.Build(ctx, req.ClusterID, req.Ref, req.Prompt)
	if err != nil {
		return domain.AIContextPreview{}, err
	}
	return built.Preview, nil
}

// CancelAskAI stops one session.
func (s *Service) CancelAskAI(sessionID string) {
	s.runner.Cancel(sessionID)
}
