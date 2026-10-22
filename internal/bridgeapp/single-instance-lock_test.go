package bridgeapp

import (
	"errors"
	"testing"
)

func TestSecondInstanceIsRejected(t *testing.T) {
	home := t.TempDir() // os.UserConfigDir: $HOME/Library/Application Support, $XDG_CONFIG_HOME or %AppData%
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("AppData", home)
	release, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("want ErrAlreadyRunning, got %v", err)
	}
	release()
	again, err := AcquireLock()
	if err != nil {
		t.Fatalf("lock must be reusable after release: %v", err)
	}
	again()
}
