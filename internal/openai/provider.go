// Package openai implements Lum Harbor's built-in OpenAI-compatible provider
// behind the provider-neutral ai.Provider contract.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"lum-harbor-notifier/internal/ai"
)

var (
	ErrAuthentication    = errors.New("OpenAI authentication failed")
	ErrRequestFailed     = errors.New("OpenAI request failed")
	ErrMalformedResponse = errors.New("OpenAI returned a malformed response")
)

type Config struct {
	Endpoint   string
	Model      string
	Credential string
	Timeout    time.Duration
	Client     *http.Client
}

type Provider struct {
	endpoint   string
	model      string
	credential string
	client     *http.Client
}

func NewProvider(config Config) (*Provider, error) {
	if strings.TrimSpace(config.Endpoint) == "" || strings.TrimSpace(config.Model) == "" || strings.TrimSpace(config.Credential) == "" {
		return nil, fmt.Errorf("construct built-in OpenAI provider: endpoint, model, and credential are required")
	}
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("construct built-in OpenAI provider: timeout must be positive")
	}
	client := config.Client
	if client == nil {
		// DisableKeepAlives avoids a real, reproducible Go net/http defect
		// ("Unsolicited response received on idle HTTP channel") observed
		// against a genuine external OpenAI-compatible endpoint during M8
		// external acceptance: an early 4xx response can arrive while the
		// connection is still considered idle by the pool, and the
		// transport discards it instead of returning it as this request's
		// response. A one-shot Verify/Execute call has no meaningful
		// keep-alive benefit, so a fresh connection per call is the
		// smallest fix that reliably surfaces the real response.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DisableKeepAlives = true
		client = &http.Client{Timeout: config.Timeout, Transport: transport}
	}
	return &Provider{
		endpoint: strings.TrimRight(config.Endpoint, "/"), model: config.Model,
		credential: config.Credential, client: client,
	}, nil
}

// redact defends diagnostic logging against a misbehaving endpoint that
// echoes the submitted credential back into its own response body; a
// compliant OpenAI-compatible server never does this, but logging must not
// depend on the far end's good behavior.
func (p *Provider) redact(text string) string {
	if p.credential != "" {
		text = strings.ReplaceAll(text, p.credential, "[REDACTED]")
	}
	return ai.SafeLogText(text, 2048)
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	ResponseFormat responseFormat `json:"response_format"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type jsonSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (p *Provider) Execute(ctx context.Context, request ai.Request) (ai.Response, error) {
	messages := make([]chatMessage, len(request.Messages))
	for index, message := range request.Messages {
		messages[index] = chatMessage{Role: string(message.Role), Content: message.Content}
	}
	payload, err := json.Marshal(chatRequest{
		Model: p.model, Messages: messages,
		ResponseFormat: responseFormat{Type: "json_schema", JSONSchema: jsonSchema{Name: "lum_harbor_response", Strict: true, Schema: request.OutputSchema}},
	})
	if err != nil {
		return ai.Response{}, fmt.Errorf("%w: encode request", ErrRequestFailed)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ai.Response{}, fmt.Errorf("%w: construct request", ErrRequestFailed)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.credential)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(httpRequest)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ai.Response{}, fmt.Errorf("%w: %w", ErrRequestFailed, ctxErr)
		}
		// Safe: a Go net/http transport-level error never contains the
		// Authorization header or response body, only connection diagnostics.
		slog.Warn("built-in OpenAI request transport failure", "error", p.redact(err.Error()))
		return ai.Response{}, ErrRequestFailed
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return ai.Response{}, ErrAuthentication
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Bounded, safe diagnostic: a compliant OpenAI-compatible error body
		// describes the request problem (e.g. an unsupported response_format
		// keyword) and never echoes the Authorization header/credential.
		// Redacted defensively anyway in case a misbehaving endpoint ever
		// echoed the credential value back into its own response body.
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2<<10))
		slog.Warn("built-in OpenAI request rejected", "status", response.StatusCode, "bodyPreview", p.redact(string(body)))
		return ai.Response{}, fmt.Errorf("%w: HTTP status %d", ErrRequestFailed, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	var decoded chatResponse
	if err := decoder.Decode(&decoded); err != nil || len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		slog.Warn("built-in OpenAI response could not be parsed", "decodeError", err, "choiceCount", len(decoded.Choices))
		return ai.Response{}, ErrMalformedResponse
	}
	output := json.RawMessage(decoded.Choices[0].Message.Content)
	usage := &ai.Usage{InputTokens: decoded.Usage.PromptTokens, OutputTokens: decoded.Usage.CompletionTokens, TotalTokens: decoded.Usage.TotalTokens}
	return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: output, Usage: usage}, nil
}
