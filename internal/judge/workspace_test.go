package judge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceTrust(t *testing.T) {
	for _, mode := range []os.FileMode{0755, 0777, 0710} {
		root := t.TempDir()
		if err := os.Chmod(root, mode); err != nil {
			t.Fatal(err)
		}
		if err := PrepareWorkspace(root); err == nil {
			t.Fatalf("accepted mode %o", mode)
		}
		if err := ReapWorkspaces(root, time.Now()); err == nil {
			t.Fatal("maintenance accepted insecure root")
		}
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "work")
	if err := PrepareWorkspace(root); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWorkspace(root); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "alias")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWorkspace(link); err == nil {
		t.Fatal("accepted root symlink")
	}
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0700)
	if err := PrepareWorkspace(root); err == nil {
		t.Fatal("accepted replaceable ancestor")
	}
}
func TestSourceCreationDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "main.py")
	if err := os.Symlink(target, source); err != nil {
		t.Fatal(err)
	}
	if err := writeSource(source, "overwrite"); err == nil {
		t.Fatal("source followed symlink")
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "preserve" {
		t.Fatalf("target altered: %q %v", b, err)
	}
}

func secureWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}
