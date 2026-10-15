//go:build darwin

package credential

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>

static CFStringRef lh_string(const char *value) {
    return CFStringCreateWithCString(kCFAllocatorDefault, value, kCFStringEncodingUTF8);
}

static CFMutableDictionaryRef lh_query(const char *service, const char *account) {
    CFStringRef serviceValue = lh_string(service);
    CFStringRef accountValue = lh_string(account);
    if (serviceValue == NULL || accountValue == NULL) {
        if (serviceValue != NULL) CFRelease(serviceValue);
        if (accountValue != NULL) CFRelease(accountValue);
        return NULL;
    }
    CFMutableDictionaryRef query = CFDictionaryCreateMutable(
        kCFAllocatorDefault, 0, &kCFTypeDictionaryKeyCallBacks,
        &kCFTypeDictionaryValueCallBacks);
    if (query != NULL) {
        CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
        CFDictionarySetValue(query, kSecAttrService, serviceValue);
        CFDictionarySetValue(query, kSecAttrAccount, accountValue);
    }
    CFRelease(serviceValue);
    CFRelease(accountValue);
    return query;
}

static OSStatus lh_resolve(const char *service, const char *account,
                           void **passwordData, CFIndex *passwordLen) {
    CFMutableDictionaryRef query = lh_query(service, account);
    if (query == NULL) return errSecAllocate;
    CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
    CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);
    CFTypeRef result = NULL;
    OSStatus status = SecItemCopyMatching(query, &result);
    CFRelease(query);
    if (status != errSecSuccess) return status;
    CFDataRef data = (CFDataRef)result;
    CFIndex length = CFDataGetLength(data);
    void *copy = NULL;
    if (length > 0) {
        copy = malloc((size_t)length);
        if (copy == NULL) {
            CFRelease(result);
            return errSecAllocate;
        }
        CFDataGetBytes(data, CFRangeMake(0, length), copy);
    }
    CFRelease(result);
    *passwordData = copy;
    *passwordLen = length;
    return errSecSuccess;
}

static OSStatus lh_set(const char *service, const char *account,
                       const void *password, CFIndex passwordLen) {
    CFDataRef passwordValue = CFDataCreate(kCFAllocatorDefault, password, passwordLen);
    if (passwordValue == NULL) return errSecAllocate;
    CFMutableDictionaryRef query = lh_query(service, account);
    if (query == NULL) {
        CFRelease(passwordValue);
        return errSecAllocate;
    }
    const void *updateKeys[] = { kSecValueData };
    const void *updateValues[] = { passwordValue };
    CFDictionaryRef update = CFDictionaryCreate(
        kCFAllocatorDefault, updateKeys, updateValues, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    OSStatus status = SecItemUpdate(query, update);
    CFRelease(update);
    if (status == errSecItemNotFound) {
        CFDictionarySetValue(query, kSecValueData, passwordValue);
        status = SecItemAdd(query, NULL);
    }
    CFRelease(query);
    CFRelease(passwordValue);
    return status;
}

static OSStatus lh_delete(const char *service, const char *account) {
    CFMutableDictionaryRef query = lh_query(service, account);
    if (query == NULL) return errSecAllocate;
    OSStatus status = SecItemDelete(query);
    CFRelease(query);
    return status;
}
*/
import "C"

import (
	"context"
	"fmt"
	"unsafe"
)

const (
	openAIKeychainService = "com.lumharbor.desktop"
	openAIKeychainAccount = "builtin-openai-api-key"
)

type keychainOpenAIStore struct{}

func newPlatformOpenAIStore(useSystemCredentialManager bool) OpenAIStore {
	if useSystemCredentialManager {
		return keychainOpenAIStore{}
	}
	return newApplicationManagedOpenAIStore()
}

func (keychainOpenAIStore) Set(ctx context.Context, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	service, account := C.CString(openAIKeychainService), C.CString(openAIKeychainAccount)
	password := C.CBytes([]byte(value))
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	defer C.free(password)
	status := C.lh_set(service, account, password, C.CFIndex(len(value)))
	if status != C.errSecSuccess {
		return fmt.Errorf("store built-in OpenAI credential in macOS Keychain: status %d", int(status))
	}
	return nil
}

func (keychainOpenAIStore) Exists(ctx context.Context) (bool, error) {
	_, exists, err := (keychainOpenAIStore{}).Resolve(ctx)
	return exists, err
}

func (keychainOpenAIStore) Resolve(ctx context.Context) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	service, account := C.CString(openAIKeychainService), C.CString(openAIKeychainAccount)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	var length C.CFIndex
	var data unsafe.Pointer
	status := C.lh_resolve(service, account, &data, &length)
	if status == C.errSecItemNotFound {
		return "", false, nil
	}
	if status != C.errSecSuccess {
		return "", false, fmt.Errorf("resolve built-in OpenAI credential from macOS Keychain: status %d", int(status))
	}
	if data != nil {
		defer C.free(data)
	}
	return string(C.GoBytes(data, C.int(length))), true, nil
}

func (keychainOpenAIStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	service, account := C.CString(openAIKeychainService), C.CString(openAIKeychainAccount)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	status := C.lh_delete(service, account)
	if status == C.errSecItemNotFound {
		return nil
	}
	if status != C.errSecSuccess {
		return fmt.Errorf("delete built-in OpenAI credential from macOS Keychain: status %d", int(status))
	}
	return nil
}
