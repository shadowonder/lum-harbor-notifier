package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const (
	MethodHandshake        = "system.handshake"
	MethodShutdown         = "system.shutdown"
	MethodCapabilityInvoke = "capability.invoke"
)

type rpcRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type HandshakeRequest struct {
	ProtocolVersion  int            `json:"protocolVersion"`
	ExtensionID      string         `json:"extensionId"`
	ExtensionVersion string         `json:"extensionVersion"`
	Capabilities     []CapabilityID `json:"capabilities"`
}

type HandshakeResult struct {
	ProtocolVersion  int            `json:"protocolVersion"`
	ExtensionID      string         `json:"extensionId"`
	ExtensionVersion string         `json:"extensionVersion"`
	Capabilities     []CapabilityID `json:"capabilities"`
}

type CapabilityInvokeParams struct {
	CapabilityID CapabilityID    `json:"capabilityId"`
	InstanceID   string          `json:"instanceId,omitempty"`
	Config       json.RawMessage `json:"config,omitempty"`
	Input        json.RawMessage `json:"input"`
}

func decodeRPCResponse(line []byte) (rpcResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var response rpcResponse
	if err := decoder.Decode(&response); err != nil {
		return rpcResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return rpcResponse{}, err
	}
	if response.ID == "" {
		return rpcResponse{}, fmt.Errorf("response id must not be empty")
	}
	hasResult := len(response.Result) > 0
	hasError := response.Error != nil
	if hasResult == hasError {
		return rpcResponse{}, fmt.Errorf("response must contain exactly one of result or error")
	}
	return response, nil
}

func ensureDecoderEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}
