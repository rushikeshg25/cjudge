//go:build sandboxintegration

package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRealSandboxRestrictions(t *testing.T) {
	if os.Getenv("CJUDGE_TEST_DOCKER") != "1" {
		t.Skip("set CJUDGE_TEST_DOCKER=1")
	}
	runtime := os.Getenv("CJUDGE_TEST_RUNTIME")
	if runtime == "" {
		runtime = "runsc"
	}
	d := &Docker{Binary: "docker", Runtime: runtime}
	work, err := os.MkdirTemp("/tmp", "cjudge-isolation-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(work)
	os.Chmod(work, 0755)
	script := `import os, socket
assert os.getuid() == 65532
for path in ['/work/write', '/root/write', '/etc/write']:
 try:
  open(path,'w').write('bad')
 except OSError:
  pass
 else:
  raise Exception('writable host/root')
try:
 socket.create_connection(('1.1.1.1', 53), 0.5)
except OSError:
 pass
else:
 raise Exception('network enabled')
assert not os.path.exists('/var/run/docker.sock')
assert 'DATABASE_URL' not in os.environ
print('isolated')`
	out, err := d.Run(context.Background(), Request{Image: "cjudge-python:1", Command: []string{"python3", "-I", "-c", script}, Workspace: work, Timeout: 5 * time.Second, MemoryMB: 128, OutputBytes: 4096})
	if err != nil || out.ExitCode != 0 || strings.TrimSpace(out.Stdout) != "isolated" {
		t.Fatalf("isolation failed: %+v %v", out, err)
	}
}
