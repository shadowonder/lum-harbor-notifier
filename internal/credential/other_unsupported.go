//go:build !darwin && !windows

package credential

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("built-in OpenAI secure credential storage is supported only on macOS and Windows")

type unsupportedOpenAIStore struct{}

func newPlatformOpenAIStore(bool) OpenAIStore                       { return unsupportedOpenAIStore{} }
func (unsupportedOpenAIStore) Set(context.Context, string) error    { return errUnsupported }
func (unsupportedOpenAIStore) Exists(context.Context) (bool, error) { return false, errUnsupported }
func (unsupportedOpenAIStore) Delete(context.Context) error         { return errUnsupported }
func (unsupportedOpenAIStore) Resolve(context.Context) (string, bool, error) {
	return "", false, errUnsupported
}
