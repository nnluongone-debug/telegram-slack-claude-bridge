//go:build darwin

package uploads

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// quarantine marks a saved file the way browsers mark downloads, so Gatekeeper
// checks it if someone opens it from Finder later.
func quarantine(path string) error {
	value := fmt.Sprintf("0083;%x;Claude Bridge;", time.Now().Unix())
	if err := unix.Setxattr(path, "com.apple.quarantine", []byte(value), 0); err != nil {
		return fmt.Errorf("set quarantine attribute: %w", err)
	}
	return nil
}
