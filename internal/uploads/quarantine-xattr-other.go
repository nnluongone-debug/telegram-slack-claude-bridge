//go:build !darwin

package uploads

// quarantine is a no-op outside macOS: files are saved without the execute bit.
func quarantine(string) error { return nil }
