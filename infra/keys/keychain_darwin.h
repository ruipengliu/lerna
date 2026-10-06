#include <stddef.h>
int lerna_keychain_disable_ui(void);
int lerna_keychain_open(const char *path);
int lerna_keychain_read(const char *path, const char *account, unsigned char **data, size_t *size);
int lerna_keychain_create(const char *path, const unsigned char *password, size_t size);
int lerna_keychain_put(const char *path, const char *account, const unsigned char *secret, size_t size);
int lerna_keychain_put_executable(const char *path, const char *account, const unsigned char *secret, size_t size, const char *executable);
int lerna_keychain_lock(const char *path);
int lerna_keychain_unlock(const char *path, const unsigned char *password, size_t size);
int lerna_keychain_delete(const char *path);
int lerna_keychain_remove(const char *path, const char *account);
void lerna_keychain_free(unsigned char *data, size_t size);
