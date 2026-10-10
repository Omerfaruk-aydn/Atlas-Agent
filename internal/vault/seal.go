package vault

// Seal protects a small secret with the platform credential protection the
// vault itself uses (DPAPI on Windows, ATLAS_VAULT_KEY elsewhere).
func Seal(data []byte) ([]byte, error) { return seal(data) }

// Unseal reverses Seal.
func Unseal(data []byte) ([]byte, error) { return unseal(data) }
