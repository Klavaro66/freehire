package handler

import (
	"context"
	"testing"

	"github.com/tmc/langchaingo/llms"

	"github.com/strelov1/freehire/internal/assistant"
	"github.com/strelov1/freehire/internal/db"
	"github.com/strelov1/freehire/internal/llm"
	"github.com/strelov1/freehire/internal/llmkey"
)

// stubKeyQueries is the smallest store the resolver needs, for the handler-specific tests
// below (llmkey.Bind's own behavior, including the claim/clear paths, is tested in
// internal/llmkey/bind_test.go — this stub only has to compile against llmkey.Queries).
type stubKeyQueries struct {
	stored map[int64]string
}

func newStubKeyQueries() *stubKeyQueries {
	return &stubKeyQueries{stored: map[int64]string{}}
}

func (s *stubKeyQueries) GetUserLLMKey(_ context.Context, id int64) (string, error) {
	return s.stored[id], nil
}

func (s *stubKeyQueries) ClaimUserLLMKey(_ context.Context, arg db.ClaimUserLLMKeyParams) (string, error) {
	s.stored[arg.ID] = arg.LlmKey.String
	return arg.LlmKey.String, nil
}

func (s *stubKeyQueries) ClearUserLLMKey(_ context.Context, arg db.ClearUserLLMKeyParams) error {
	delete(s.stored, arg.ID)
	return nil
}

// The typed-nil trap, caught in production by the integration suite and not by a unit test
// that passed an untyped nil. llmkey.Bind returns *llm.Client; assigning a nil one into the
// runner's Model INTERFACE produces a non-nil interface holding a nil pointer, and the
// runner then dereferences it on its first round — inside the stream goroutine, after the
// response has begun.
func TestBoundRunnerKeepsTheOriginalWhenNoClientResolves(t *testing.T) {
	original := assistant.NewRunner(&nilSafeModel{}, assistant.NewStore(nil), assistant.RunnerConfig{MaxSteps: 3})
	h := &assistantHandlers{
		runner: original,
		keys:   llmkey.NewResolver(newStubKeyQueries(), nil),
		// llm is nil: this deployment has no assistant model client to re-credential.
	}

	if got := h.boundRunner(context.Background(), assistant.Session{UserID: 7, Preset: assistant.PresetChat}); got != original {
		t.Error("an unresolved client replaced the runner's model with a typed nil; the turn would panic mid-stream")
	}
}

// nilSafeModel stands in for the assistant's model in a runner nobody is going to run.
type nilSafeModel struct{}

func (nilSafeModel) Chat(context.Context, []llms.MessageContent, []llms.Tool, llm.ChatStream) (*llms.ContentChoice, error) {
	return &llms.ContentChoice{Content: "unused"}, nil
}
