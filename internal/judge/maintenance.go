package judge

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReapWorkspaces recovers source/artifact directories after a process crash.
// 32 minutes exceeds the maximum configurable job deadline plus cleanup grace.
func ReapWorkspaces(root string, now time.Time) error {
	if err := PrepareWorkspace(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "job-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) <= 32*time.Minute {
			continue
		}
		if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
