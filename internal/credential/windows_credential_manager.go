//go:build windows

package credential

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// Windows production credential store: the built-in Windows Credential
// Manager (advapi32.dll CredWriteW/CredReadW/CredDeleteW/CredFree), called
// directly via the Go standard library's syscall package.
//
// This was chosen over a raw DPAPI (CryptProtectData/CryptUnprotectData)
// implementation because Credential Manager already provides the full
// store/read/delete/enumerate lifecycle Lum Harbor needs (matching the
// Set/Exists/Delete/Resolve shape of the macOS Keychain store almost
// directly), is the same current-user-scoped, OS-integrated credential
// vault end users already see in Windows' own "Credential Manager" control
// panel, and needs no separate on-disk blob or file-permission story of its
// own the way a hand-rolled DPAPI-encrypted file would. A generic secret
// framework was deliberately not built: this file exposes exactly one
// unexported store type satisfying the existing OpenAIStore interface.
//
// Everything here uses syscall.NewLazyDLL/NewProc against advapi32.dll, not
// CGO and not golang.org/x/sys, so the existing CGO_ENABLED=0 Windows
// cross-build story from the platform baseline round is preserved.

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = 1168

	credentialTargetName = "LumHarbor:BuiltInOpenAIAPIKey"
)

var (
	modAdvapi32     = syscall.NewLazyDLL("advapi32.dll")
	procCredWriteW  = modAdvapi32.NewProc("CredWriteW")
	procCredReadW   = modAdvapi32.NewProc("CredReadW")
	procCredDeleteW = modAdvapi32.NewProc("CredDeleteW")
	procCredFree    = modAdvapi32.NewProc("CredFree")
)

// credentialW mirrors the Win32 CREDENTIALW struct layout required by
// CredWriteW/CredReadW. Field order and sizes must match the platform ABI
// exactly; this is not a general-purpose binding, only what Set/Resolve/
// Delete need.
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        [2]uint32 // FILETIME
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsCredentialManagerStore struct{}

func newPlatformOpenAIStore(bool) OpenAIStore { return windowsCredentialManagerStore{} }

func (windowsCredentialManagerStore) Set(ctx context.Context, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value == "" {
		return errors.New("windows credential manager value must not be empty")
	}
	target, err := syscall.UTF16PtrFromString(credentialTargetName)
	if err != nil {
		return fmt.Errorf("encode credential target name: %w", err)
	}
	blob := []byte(value)
	cred := credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
	}
	r1, _, lastErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r1 == 0 {
		return fmt.Errorf("store built-in OpenAI credential in Windows Credential Manager: %w", lastErr)
	}
	return nil
}

func (s windowsCredentialManagerStore) Exists(ctx context.Context) (bool, error) {
	_, exists, err := s.Resolve(ctx)
	return exists, err
}

func (windowsCredentialManagerStore) Resolve(ctx context.Context) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	target, err := syscall.UTF16PtrFromString(credentialTargetName)
	if err != nil {
		return "", false, fmt.Errorf("encode credential target name: %w", err)
	}
	// cred is populated in place by CredReadW writing a pointer value into
	// &cred. This avoids ever converting a returned uintptr back into
	// unsafe.Pointer (which go vet's unsafeptr check correctly treats as
	// generally unsound); the only unsafe.Pointer conversions here are of
	// addresses of our own live local variables, which is always valid.
	var cred *credentialW
	r1, _, lastErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(credTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&cred)),
	)
	if r1 == 0 {
		if errno, ok := lastErr.(syscall.Errno); ok && uintptr(errno) == errorNotFound {
			return "", false, nil
		}
		return "", false, fmt.Errorf("resolve built-in OpenAI credential from Windows Credential Manager: %w", lastErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	if cred.CredentialBlobSize == 0 || cred.CredentialBlob == nil {
		return "", true, nil
	}
	blob := unsafe.Slice(cred.CredentialBlob, int(cred.CredentialBlobSize))
	value := make([]byte, len(blob))
	copy(value, blob)
	return string(value), true, nil
}

func (windowsCredentialManagerStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(credentialTargetName)
	if err != nil {
		return fmt.Errorf("encode credential target name: %w", err)
	}
	r1, _, lastErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), uintptr(credTypeGeneric), 0)
	if r1 == 0 {
		if errno, ok := lastErr.(syscall.Errno); ok && uintptr(errno) == errorNotFound {
			return nil
		}
		return fmt.Errorf("delete built-in OpenAI credential from Windows Credential Manager: %w", lastErr)
	}
	return nil
}
