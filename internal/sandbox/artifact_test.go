package sandbox

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactRejectsSymlinks(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	tw.WriteHeader(&tar.Header{Name: "program", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	tw.Close()
	if err := extractArtifact(&b, filepath.Join(t.TempDir(), "program")); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestArtifactRegularFile(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	tw.WriteHeader(&tar.Header{Name: "../../escape", Typeflag: tar.TypeReg, Size: 3, Mode: 0755})
	tw.Write([]byte("exe"))
	tw.Close()
	p := filepath.Join(t.TempDir(), "program")
	if err := extractArtifact(&b, p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "exe" {
		t.Fatal("artifact mismatch")
	}
}
