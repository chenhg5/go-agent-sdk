// Package claude implements the agentsdk.Provider interface for the
// Claude Messages API (https://docs.anthropic.com/en/api/messages).
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	agentsdk "github.com/chenhg5/go-agent-sdk"
)

// Provider talks to the Claude Messages API.
type Provider struct {
	apiKey string
	opts   options
}

var _ agentsdk.Provider = (*Provider)(nil)

// NewProvider creates a Provider with the given API key.
func NewProvider(apiKey string, opts ...Option) *Provider {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return &Provider{apiKey: apiKey, opts: o}
}

// ---------------------------------------------------------------------------
// agentsdk.Provider
// ---------------------------------------------------------------------------

func (p *Provider) CreateMessage(ctx context.Context, params *agentsdk.MessageParams) (*agentsdk.MessageResponse, error) {
	body, err := p.marshalRequest(params, false)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= p.opts.maxRetries; attempt++ {
		if attempt > 0 {
			if !isRetryable(lastErr) {
				break
			}
			sleepWithContext(ctx, attempt)
		}
		resp, err := p.doHTTP(ctx, body)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = parseErrorResponse(resp)
			continue
		}
		defer resp.Body.Close()
		var msg agentsdk.MessageResponse
		if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
			return nil, fmt.Errorf("claude: decode response: %w", err)
		}
		return &msg, nil
	}
	return nil, lastErr
}

func (p *Provider) CreateMessageStream(ctx context.Context, params *agentsdk.MessageParams) (agentsdk.Stream, error) {
	body, err := p.marshalRequest(params, true)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= p.opts.maxRetries; attempt++ {
		if attempt > 0 {
			if !isRetryable(lastErr) {
				break
			}
			sleepWithContext(ctx, attempt)
		}
		resp, err := p.doHTTP(ctx, body)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = parseErrorResponse(resp)
			continue
		}
		return newStream(resp.Body), nil
	}
	return nil, lastErr
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

func (p *Provider) doHTTP(ctx context.Context, body []byte) (*http.Response, error) {
	url := strings.TrimRight(p.opts.baseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("claude: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", p.opts.apiVersion)
	if len(p.opts.betaFeatures) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(p.opts.betaFeatures, ","))
	}
	for k, v := range p.opts.extraHeaders {
		req.Header.Set(k, v)
	}
	return p.opts.httpClient.Do(req)
}

// ---------------------------------------------------------------------------
// Request serialisation
// ---------------------------------------------------------------------------

type apiRequest struct {
	Model         string               `json:"model"`
	Messages      []agentsdk.Message   `json:"messages"`
	System        json.RawMessage      `json:"system,omitempty"`
	MaxTokens     int                  `json:"max_tokens"`
	Stream        bool                 `json:"stream,omitempty"`
	Tools         []agentsdk.ToolSpec  `json:"tools,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	TopK          *int                 `json:"top_k,omitempty"`
	StopSequences []string             `json:"stop_sequences,omitempty"`
	ToolChoice    *agentsdk.ToolChoice `json:"tool_choice,omitempty"`
	Thinking      *thinkingWire        `json:"thinking,omitempty"`
}

type thinkingWire struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

func (p *Provider) marshalRequest(params *agentsdk.MessageParams, stream bool) ([]byte, error) {
	r := apiRequest{
		Model: params.Model, Messages: params.Messages,
		MaxTokens: params.MaxTokens,
		Stream: stream, Tools: params.Tools,
		Temperature: params.Temperature, TopP: params.TopP, TopK: params.TopK,
		StopSequences: params.StopSequences, ToolChoice: params.ToolChoice,
	}

	// System prompt: structured blocks (with cache control) take precedence over plain string.
	if len(params.SystemBlocks) > 0 {
		if p.opts.forceStringSystem {
			// Flatten blocks into a single string for proxies that don't support the array format.
			var sb strings.Builder
			for i, b := range params.SystemBlocks {
				if i > 0 {
					sb.WriteString("\n\n")
				}
				sb.WriteString(b.Text)
			}
			raw, _ := json.Marshal(sb.String())
			r.System = raw
		} else {
			raw, err := json.Marshal(params.SystemBlocks)
			if err != nil {
				return nil, fmt.Errorf("claude: marshal system blocks: %w", err)
			}
			r.System = raw
		}
	} else if params.System != "" {
		raw, err := json.Marshal(params.System)
		if err != nil {
			return nil, fmt.Errorf("claude: marshal system prompt: %w", err)
		}
		r.System = raw
	}

	if params.Thinking != nil && params.Thinking.BudgetTokens > 0 {
		r.Thinking = &thinkingWire{
			Type:         params.Thinking.Type,
			BudgetTokens: params.Thinking.BudgetTokens,
		}
	}
	return json.Marshal(r)
}

// ---------------------------------------------------------------------------
// Retry
// ---------------------------------------------------------------------------

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.IsRetryable()
	}
	return true
}

func sleepWithContext(ctx context.Context, attempt int) {
	d := time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
