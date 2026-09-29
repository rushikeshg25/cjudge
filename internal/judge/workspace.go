package judge

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// PrepareWorkspace fails closed on roots that could expose source or allow a
// different host user to replace job paths. Root and the worker UID are trusted;
// ACLs and the Docker daemon remain deployment-level trust boundaries.
func PrepareWorkspace(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) == "/" {
		return fmt.Errorf("workspace must be a dedicated absolute directory")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("workspace must be a real directory owned by worker UID %d with mode 0700", os.Geteuid())
	}
	// Check both the spelling supplied by the operator and resolved paths. This
	// permits system aliases such as macOS /tmp, but not attacker-owned aliases.
	for path := filepath.Dir(root); ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (st.Uid != 0 && int(st.Uid) != os.Geteuid()) {
			return fmt.Errorf("untrusted workspace ancestor: %s", path)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		for ancestor := resolved; ; ancestor = filepath.Dir(ancestor) {
			info, err := os.Stat(ancestor)
			if err != nil {
				return err
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || !info.IsDir() || (st.Uid != 0 && int(st.Uid) != os.Geteuid()) || (info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) {
				return fmt.Errorf("replaceable workspace ancestor: %s", ancestor)
			}
			if ancestor == filepath.Dir(ancestor) {
				break
			}
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	return nil
}

func writeSource(path, source string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0444)
	if err != nil {
		return err
	}
	_, err = f.WriteString(source)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
