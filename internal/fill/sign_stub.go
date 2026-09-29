//go:build !darwin

package fill

// codesignHost is a no-op off darwin — no keychain, no codesign.
func codesignHost(path string) error {
	_ = path
	return nil
}
