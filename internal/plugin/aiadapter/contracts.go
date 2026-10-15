package aiadapter

import (
	"encoding/json"

	"lum-harbor-notifier/internal/ai"
)

type message struct {
	Role    ai.Role `json:"role"`
	Content string  `json:"content"`
}

type request struct {
	RequestID    string            `json:"requestId"`
	Action       ai.Action         `json:"action"`
	Messages     []message         `json:"messages"`
	OutputSchema json.RawMessage   `json:"outputSchema"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type response struct {
	RequestID string            `json:"requestId"`
	Action    ai.Action         `json:"action"`
	Output    json.RawMessage   `json:"output"`
	Usage     *ai.Usage         `json:"usage,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type preResult struct {
	Decision ai.Decision `json:"decision"`
	Request  *request    `json:"request,omitempty"`
	Response *response   `json:"response,omitempty"`
}

type postResult struct {
	Decision ai.Decision `json:"decision"`
	Response *response   `json:"response,omitempty"`
}

func requestToWire(value ai.Request) request {
	messages := make([]message, len(value.Messages))
	for index, item := range value.Messages {
		messages[index] = message{Role: item.Role, Content: item.Content}
	}
	return request{
		RequestID: value.RequestID, Action: value.Action, Messages: messages,
		OutputSchema: append(json.RawMessage(nil), value.OutputSchema...),
		Metadata:     cloneMap(value.Metadata),
	}
}

func requestFromWire(value request) ai.Request {
	messages := make([]ai.Message, len(value.Messages))
	for index, item := range value.Messages {
		messages[index] = ai.Message{Role: item.Role, Content: item.Content}
	}
	return ai.Request{
		RequestID: value.RequestID, Action: value.Action, Messages: messages,
		OutputSchema: append(json.RawMessage(nil), value.OutputSchema...),
		Metadata:     cloneMap(value.Metadata),
	}
}

func responseToWire(value ai.Response) response {
	var usage *ai.Usage
	if value.Usage != nil {
		copy := *value.Usage
		usage = &copy
	}
	return response{
		RequestID: value.RequestID, Action: value.Action,
		Output: append(json.RawMessage(nil), value.Output...),
		Usage:  usage, Metadata: cloneMap(value.Metadata),
	}
}

func responseFromWire(value response) ai.Response {
	var usage *ai.Usage
	if value.Usage != nil {
		copy := *value.Usage
		usage = &copy
	}
	return ai.Response{
		RequestID: value.RequestID, Action: value.Action,
		Output: append(json.RawMessage(nil), value.Output...),
		Usage:  usage, Metadata: cloneMap(value.Metadata),
	}
}

func cloneMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
