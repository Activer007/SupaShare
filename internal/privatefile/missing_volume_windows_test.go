//go:build windows

package privatefile

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMissingVolumeTerminatesParentWalk(t *testing.T) {
	for drive := 'Z'; drive >= 'D'; drive-- {
		root := fmt.Sprintf("%c:\\", drive)
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			continue
		}
		done := make(chan error, 1)
		go func() { _, err := Exists(root + "missing-private-dir\\config.json"); done <- err }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("missing drive root did not terminate ancestor traversal")
		}
		return
	}
	t.Skip("no nonexistent drive available for native traversal regression")
}
