package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/strelov1/freehire/internal/autoapply"
)

// sidecarSubmitRequest is the wire shape services/auto-apply expects at POST /submit.
type sidecarSubmitRequest struct {
	JobURL   string            `json:"job_url"`
	Provider string            `json:"provider"`
	Answers  map[string]string `json:"answers"`
}

// sidecarUnmappedField mirrors autoapply.UnmappedField over the wire.
type sidecarUnmappedField struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Reason   string `json:"reason"`
}

// sidecarSubmitResponse is what services/auto-apply returns for one attempt.
type sidecarSubmitResponse struct {
	Status   string                 `json:"status"`
	Unmapped []sidecarUnmappedField `json:"unmapped,omitempty"`
	Reason   string                 `json:"reason,omitempty"`
}

// httpSidecarClient calls services/auto-apply over HTTP. The only place this binary knows
// the sidecar's wire shape — internal/autoapply sees only the already-decoded
// autoapply.SidecarResult.
type httpSidecarClient struct {
	baseURL string
	client  *http.Client
}

func newHTTPSidecarClient(baseURL string, client *http.Client) *httpSidecarClient {
	return &httpSidecarClient{baseURL: baseURL, client: client}
}

var _ autoapply.SidecarClient = (*httpSidecarClient)(nil)

func (c *httpSidecarClient) Submit(ctx context.Context, jobURL, provider string, answers map[string]string) (autoapply.SidecarResult, error) {
	body, err := json.Marshal(sidecarSubmitRequest{JobURL: jobURL, Provider: provider, Answers: answers})
	if err != nil {
		return autoapply.SidecarResult{}, fmt.Errorf("encode sidecar request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/submit", bytes.NewReader(body))
	if err != nil {
		return autoapply.SidecarResult{}, fmt.Errorf("build sidecar request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return autoapply.SidecarResult{}, fmt.Errorf("call auto-apply sidecar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return autoapply.SidecarResult{}, fmt.Errorf("auto-apply sidecar returned %d: %s", resp.StatusCode, snippet)
	}

	var out sidecarSubmitResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return autoapply.SidecarResult{}, fmt.Errorf("decode sidecar response: %w", err)
	}

	unmapped := make([]autoapply.UnmappedField, 0, len(out.Unmapped))
	for _, u := range out.Unmapped {
		unmapped = append(unmapped, autoapply.UnmappedField{
			ID: u.ID, Label: u.Label, Required: u.Required, Reason: u.Reason,
		})
	}
	return autoapply.SidecarResult{
		Status:   autoapply.SubmitStatus(out.Status),
		Unmapped: unmapped,
		Reason:   out.Reason,
	}, nil
}
