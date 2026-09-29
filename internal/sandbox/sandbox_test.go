package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestMandatoryRestrictions(t *testing.T) {
	args, err := createArgs("test", "runsc", Request{Image: "image", Command: []string{"./main"}, Workspace: "/tmp/work", Timeout: time.Second, MemoryMB: 64, OutputBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--pids-limit=64", "--memory=64m", "--memory-swap=64m", "--user=65532:65532", "dst=/work,readonly", "--runtime runsc", "--log-driver=none", "--pull=never"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s", want)
		}
	}
}
