// Package plugin implements Lum Harbor's generic extension discovery,
// registry, process lifecycle, and correlated RPC transport.
package plugin

import "errors"

var (
	ErrManifestInvalid       = errors.New("plugin manifest invalid")
	ErrUnsupportedProtocol   = errors.New("plugin protocol unsupported")
	ErrExtensionNotFound     = errors.New("plugin extension not found")
	ErrInstanceNotFound      = errors.New("plugin instance not found")
	ErrCapabilityUnsupported = errors.New("plugin capability unsupported")
	ErrProcessStart          = errors.New("plugin process start failure")
	ErrHandshake             = errors.New("plugin handshake failure")
	ErrRPCCancelled          = errors.New("plugin RPC cancelled")
	ErrProtocol              = errors.New("plugin protocol failure")
	ErrRemote                = errors.New("plugin remote capability error")
	ErrHostShuttingDown      = errors.New("plugin host shutting down")
)
