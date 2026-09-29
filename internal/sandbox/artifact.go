package sandbox

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
)

// extractArtifact accepts exactly one regular file, never paths from an archive.
func extractArtifact(r io.Reader, destination string) error {
	const maxArtifact = 32 << 20
	tr := tar.NewReader(io.LimitReader(r, maxArtifact+(64<<10)))
	h, err := tr.Next()
	if err != nil {
		return err
	}
	if h.Typeflag != tar.TypeReg || h.Size < 1 || h.Size > maxArtifact {
		return fmt.Errorf("invalid compiled artifact")
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0555)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, tr)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err = tr.Next(); err != io.EOF {
		return fmt.Errorf("unexpected extra artifact")
	}
	// Drain only bounded archive padding, allowing Docker to complete its pipe.
	_, err = io.Copy(io.Discard, io.LimitReader(r, 64<<10))
	return err
}
