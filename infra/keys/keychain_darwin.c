//go:build darwin && cgo

#include "keychain_darwin.h"
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

int lerna_keychain_disable_ui(void) {
    return (int)SecKeychainSetUserInteractionAllowed(false);
}

static OSStatus open_chain(const char *path, SecKeychainRef *chain, int unlocked) {
    OSStatus status = SecKeychainOpen(path, chain);
    if (status != errSecSuccess) return status;
    if (unlocked) {
        SecKeychainStatus flags = 0;
        status = SecKeychainGetStatus(*chain, &flags);
        if (status == errSecSuccess && !(flags & kSecUnlockStateStatus)) status = errSecInteractionNotAllowed;
        if (status != errSecSuccess) { CFRelease(*chain); *chain = NULL; }
    }
    return status;
}

static CFMutableDictionaryRef query(SecKeychainRef chain, const char *account) {
    CFMutableDictionaryRef q = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFStringRef a = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
    const void *chains[] = {chain};
    CFArrayRef search = CFArrayCreate(NULL, chains, 1, &kCFTypeArrayCallBacks);
    if (!q || !a || !search) { if(q)CFRelease(q);if(a)CFRelease(a);if(search)CFRelease(search);return NULL; }
    CFDictionarySetValue(q, kSecClass, kSecClassGenericPassword);
    CFDictionarySetValue(q, kSecAttrService, CFSTR("lerna.m1.api.credentials"));
    CFDictionarySetValue(q, kSecAttrAccount, a);
    CFDictionarySetValue(q, kSecMatchSearchList, search);
    CFRelease(a);CFRelease(search);
    return q;
}

int lerna_keychain_open(const char *path) {
    SecKeychainRef chain = NULL;
    OSStatus status = open_chain(path, &chain, 1);
    if (chain) CFRelease(chain);
    return (int)status;
}

int lerna_keychain_read(const char *path, const char *account, unsigned char **data, size_t *size) {
    *data = NULL; *size = 0;
    SecKeychainRef chain = NULL;
    OSStatus status = open_chain(path, &chain, 1);
    if (status != errSecSuccess) return (int)status;
    CFMutableDictionaryRef q = query(chain, account);
    if (!q) {CFRelease(chain);return (int)errSecAllocate;}
    CFDictionarySetValue(q, kSecMatchLimit, kSecMatchLimitOne);
    CFDictionarySetValue(q, kSecReturnData, kCFBooleanTrue);
    CFTypeRef result = NULL;
    status = SecItemCopyMatching(q, &result);
    if (status == errSecSuccess) {
        if (!result || CFGetTypeID(result) != CFDataGetTypeID()) status = errSecDecode;
        else {
            CFIndex length = CFDataGetLength((CFDataRef)result);
            if (length < 16 || length > 16384) status = errSecDecode;
            else {
                *data = malloc((size_t)length);
                if (!*data) status = errSecAllocate;
                else { memcpy(*data, CFDataGetBytePtr((CFDataRef)result), (size_t)length);*size = (size_t)length; }
            }
        }
    }
    if (result) CFRelease(result);
    CFRelease(q);CFRelease(chain);
    return (int)status;
}

int lerna_keychain_create(const char *path, const unsigned char *password, size_t size) {
    SecKeychainRef chain = NULL;
    OSStatus status = SecKeychainCreate(path, (UInt32)size, password, false, NULL, &chain);
    if (chain) CFRelease(chain);
    return (int)status;
}

int lerna_keychain_put(const char *path, const char *account, const unsigned char *secret, size_t size) {
    SecKeychainRef chain = NULL;
    OSStatus status = open_chain(path, &chain, 1);
    if (status != errSecSuccess) return (int)status;
    CFMutableDictionaryRef q = query(chain, account);
    if (!q) {CFRelease(chain);return (int)errSecAllocate;}
    CFDataRef data = CFDataCreate(NULL, secret, (CFIndex)size);
    if (!data) { CFRelease(q);CFRelease(chain);return (int)errSecAllocate; }
    const void *updateKeys[] = {kSecValueData};
    const void *updateValues[] = {data};
    CFDictionaryRef update = CFDictionaryCreate(NULL, updateKeys, updateValues, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    if (!update) { CFRelease(data);CFRelease(q);CFRelease(chain);return (int)errSecAllocate; }
    status = SecItemUpdate(q, update);
    if (status == errSecItemNotFound) {
        CFDictionaryRemoveValue(q, kSecMatchSearchList);
        CFDictionarySetValue(q, kSecUseKeychain, chain);
        CFDictionarySetValue(q, kSecValueData, data);
        status = SecItemAdd(q, NULL);
    }
    CFRelease(update);CFRelease(data);CFRelease(q);CFRelease(chain);
    return (int)status;
}

int lerna_keychain_lock(const char *path) {
    SecKeychainRef chain = NULL;OSStatus status = open_chain(path, &chain, 0);
    if(status == errSecSuccess)status = SecKeychainLock(chain);
    if(chain)CFRelease(chain);return (int)status;
}

int lerna_keychain_put_executable(const char *path, const char *account, const unsigned char *secret, size_t size, const char *executable) {
    SecKeychainRef chain = NULL;
    OSStatus status = open_chain(path, &chain, 1);
    if (status != errSecSuccess) return (int)status;
    SecTrustedApplicationRef creator = NULL, host = NULL;
    SecAccessRef access = NULL;
    CFArrayRef applications = NULL;
    CFMutableDictionaryRef q = NULL;
    CFDataRef data = NULL;
    status = SecTrustedApplicationCreateFromPath(NULL, &creator);
    if (status != errSecSuccess) goto done;
    status = SecTrustedApplicationCreateFromPath(executable, &host);
    if (status != errSecSuccess) goto done;
    const void *values[] = {creator, host};
    applications = CFArrayCreate(NULL, values, 2, &kCFTypeArrayCallBacks);
    if (!applications) { status = errSecAllocate; goto done; }
    status = SecAccessCreate(CFSTR("Lerna isolated synthetic API fixture"), applications, &access);
    if (status != errSecSuccess) goto done;
    q = query(chain, account);
    data = CFDataCreate(NULL, secret, (CFIndex)size);
    if (!q || !data) { status = errSecAllocate; goto done; }
    CFDictionaryRemoveValue(q, kSecMatchSearchList);
    CFDictionarySetValue(q, kSecUseKeychain, chain);
    CFDictionarySetValue(q, kSecValueData, data);
    CFDictionarySetValue(q, kSecAttrAccess, access);
    status = SecItemAdd(q, NULL);
done:
    if(data)CFRelease(data);if(q)CFRelease(q);if(access)CFRelease(access);if(applications)CFRelease(applications);
    if(host)CFRelease(host);if(creator)CFRelease(creator);CFRelease(chain);
    return (int)status;
}
int lerna_keychain_unlock(const char *path, const unsigned char *password, size_t size) {
    SecKeychainRef chain = NULL;OSStatus status = open_chain(path, &chain, 0);
    if(status == errSecSuccess)status = SecKeychainUnlock(chain, (UInt32)size, password, true);
    if(chain)CFRelease(chain);return (int)status;
}
int lerna_keychain_delete(const char *path) {
    SecKeychainRef chain = NULL;OSStatus status = open_chain(path, &chain, 0);
    if(status == errSecSuccess)status = SecKeychainDelete(chain);
    if(chain)CFRelease(chain);return (int)status;
}
void lerna_keychain_free(unsigned char *data, size_t size) {
    if(data){volatile unsigned char *p = data;for(size_t i=0;i<size;i++)p[i]=0;free(data);}
}
int lerna_keychain_remove(const char *path, const char *account) {
    SecKeychainRef chain = NULL;OSStatus status = open_chain(path, &chain, 1);
    if(status != errSecSuccess)return (int)status;
    CFMutableDictionaryRef q = query(chain,account);
    if(!q){CFRelease(chain);return (int)errSecAllocate;}
    status = SecItemDelete(q);CFRelease(q);CFRelease(chain);return (int)status;
}
