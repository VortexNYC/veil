//go:build darwin

package replica

import (
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"github.com/VortexNYC/veil/internal/crypto"
)

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <string.h>
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>

static CFMutableDictionaryRef veil_replica_query(void) {
	CFMutableDictionaryRef q = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(q, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(q, kSecAttrService, CFSTR("nyc.veil.fill"));
	CFDictionarySetValue(q, kSecAttrAccount, CFSTR("replica"));
	return q;
}

static int veil_replica_get(void *out, int cap) {
	CFMutableDictionaryRef q = veil_replica_query();
	CFDictionarySetValue(q, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(q, kSecMatchLimit, kSecMatchLimitOne);
	// The item's ACL is user-presence. On a locked screen securityd would
	// queue an auth prompt that cannot render and block this read forever —
	// wedging every native-host child. Skip UI; fail fast, fall to origin.
	CFDictionarySetValue(q, kSecUseAuthenticationUI, kSecUseAuthenticationUISkip);
	CFTypeRef result = NULL;
	OSStatus st = SecItemCopyMatching(q, &result);
	CFRelease(q);
	if (st == errSecItemNotFound) {
		return 0;
	}
	if (st != errSecSuccess || result == NULL) {
		return -1;
	}
	CFDataRef data = (CFDataRef)result;
	CFIndex n = CFDataGetLength(data);
	if (n <= 0 || n > cap) {
		CFRelease(result);
		return -1;
	}
	memcpy(out, CFDataGetBytePtr(data), (size_t)n);
	CFRelease(result);
	return (int)n;
}

static int veil_replica_put(const void *in, int n) {
	CFMutableDictionaryRef q = veil_replica_query();
	SecItemDelete(q);
	CFDataRef data = CFDataCreate(NULL, in, n);
	if (data == NULL) {
		CFRelease(q);
		return -1;
	}
	CFDictionarySetValue(q, kSecValueData, data);
	SecAccessControlRef ac = SecAccessControlCreateWithFlags(NULL,
		kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
		kSecAccessControlUserPresence,
		NULL);
	if (ac == NULL) {
		CFRelease(data);
		CFRelease(q);
		return -1;
	}
	CFDictionarySetValue(q, kSecAttrAccessControl, ac);
	CFRelease(ac);
	OSStatus st = SecItemAdd(q, NULL);
	CFRelease(data);
	CFRelease(q);
	return st == errSecSuccess ? 0 : -1;
}
*/
import "C"

type keychain struct{}

func Platform() KeyStore {
	if os.Getenv("VEIL_REPLICA_KEYSTORE") == "mem" {
		return Mem()
	}
	return keychain{}
}

var errKeychainLocked = errors.New("replica: keychain unavailable")

// keychainReadTimeout bounds the SecItemCopyMatching call. While the screen
// is locked, securityd holds the item's presence-gated decrypt open instead
// of answering — the read would block forever and wedge the host. A bounded
// read fails to the origin path; the stranded call drains when the Mac wakes.
const keychainReadTimeout = 2 * time.Second

func (keychain) Get() ([]byte, error) {
	type result struct {
		key []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		buf := make([]byte, crypto.KeySize)
		n := C.veil_replica_get(unsafe.Pointer(&buf[0]), C.int(len(buf)))
		if n == 0 {
			done <- result{nil, ErrNotFound}
			return
		}
		if int(n) != crypto.KeySize {
			done <- result{nil, fmt.Errorf("replica: keychain")}
			return
		}
		done <- result{buf, nil}
	}()
	select {
	case r := <-done:
		return r.key, r.err
	case <-time.After(keychainReadTimeout):
		return nil, errKeychainLocked
	}
}

func (keychain) Put(key []byte) error {
	if len(key) != crypto.KeySize {
		return fmt.Errorf("replica: key")
	}
	if C.veil_replica_put(unsafe.Pointer(&key[0]), C.int(len(key))) != 0 {
		return fmt.Errorf("replica: keychain put")
	}
	return nil
}
