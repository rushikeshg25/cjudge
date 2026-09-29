package judge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceReaping(t *testing.T) {
	root := secureWorkspace(t)
	now := time.Now()
	for _, name := range []string{"job-old", "job-active", "unmanaged"} {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-time.Hour)
	os.Chtimes(filepath.Join(root, "job-old"), old, old)
	os.Chtimes(filepath.Join(root, "unmanaged"), old, old)
	if err := ReapWorkspaces(root, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "job-old")); !os.IsNotExist(err) {
		t.Fatal("orphan retained")
	}
	for _, name := range []string{"job-active", "unmanaged"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("live/unmanaged workspace removed")
		}
	}
}
