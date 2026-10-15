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
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
)

const testCredential = "super-secret-openai-key"

func TestProviderMapsCanonicalRequestAndResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+testCredential {
			t.Fatalf("unexpected request path/header: %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "gpt-test" {
			t.Fatalf("model not mapped: %s", body)
		}
		messages := payload["messages"].([]any)
		if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[1].(map[string]any)["content"] != "verify" {
			t.Fatalf("messages not mapped: %s", body)
		}
		format := payload["response_format"].(map[string]any)
		if format["type"] != "json_schema" {
			t.Fatalf("structured output missing: %s", body)
		}
		return httpResponse(http.StatusOK, `{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`), nil
	})}
	provider := mustProvider(t, "https://example.test/v1", client)
	response, err := provider.Execute(context.Background(), testRequest("one"))
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Output) != `{"ok":true}` || response.Usage == nil || response.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestProviderRedactsCredentialFromFailures(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return httpResponse(status, `{"error":"credential `+testCredential+` rejected"}`), nil
			})}
			provider := mustProvider(t, "https://example.test/v1", client)
			_, err := provider.Execute(context.Background(), testRequest("failure"))
			if err == nil || strings.Contains(err.Error(), testCredential) {
				t.Fatalf("unsafe error: %v", err)
			}
			if status == http.StatusUnauthorized && !errors.Is(err, ErrAuthentication) {
				t.Fatalf("expected authentication classification: %v", err)
			}
		})
	}
}

// TestProviderRedactsCredentialFromDiagnosticLogging locks in the M8 external
// acceptance fix: diagnostic logging for a rejected request must never leak
// the credential, even defensively against a misbehaving endpoint that
// echoes it back into its own response body (a compliant OpenAI-compatible
// server never does, but logging must not depend on that).
func TestProviderRedactsCredentialFromDiagnosticLogging(t *testing.T) {
	var logBuffer bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuffer, nil)))
	defer slog.SetDefault(previous)

	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return httpResponse(http.StatusBadRequest, `{"error":"credential `+testCredential+` rejected; password=echoed-secret"}`), nil
	})}
	provider := mustProvider(t, "https://example.test/v1", client)
	if _, err := provider.Execute(context.Background(), testRequest("logging")); err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(logBuffer.String(), testCredential) || strings.Contains(logBuffer.String(), "echoed-secret") {
		t.Fatalf("credential leaked into diagnostic log: %s", logBuffer.String())
	}
	if !strings.Contains(logBuffer.String(), "[REDACTED]") {
		t.Fatalf("expected redacted marker in diagnostic log: %s", logBuffer.String())
	}
}

func TestProviderRejectsMalformedResponseAndHonorsCancellation(t *testing.T) {
	provider := mustProvider(t, "https://example.test/v1", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"choices":[]}`), nil
	})})
	if _, err := provider.Execute(context.Background(), testRequest("malformed")); !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("expected malformed response, got %v", err)
	}

	slowClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	provider = mustProvider(t, "https://example.test/v1", slowClient)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := provider.Execute(ctx, testRequest("cancel")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestProviderIsConcurrent(t *testing.T) {
	provider := mustProvider(t, "https://example.test/v1", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`), nil
	})})
	var wg sync.WaitGroup
	for index := 0; index < 30; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if _, err := provider.Execute(context.Background(), testRequest(fmt.Sprint(index))); err != nil {
				t.Errorf("execute: %v", err)
			}
		}(index)
	}
	wg.Wait()
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func httpResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func mustProvider(t *testing.T, endpoint string, client *http.Client) *Provider {
	t.Helper()
	provider, err := NewProvider(Config{Endpoint: endpoint, Model: "gpt-test", Credential: testCredential, Timeout: time.Second, Client: client})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func testRequest(id string) ai.Request {
	return ai.Request{RequestID: id, Action: ai.ActionRuntimeVerify,
		Messages:     []ai.Message{{Role: ai.RoleSystem, Content: "return JSON"}, {Role: ai.RoleUser, Content: "verify"}},
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`),
	}
}
