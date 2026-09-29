//go:build sandboxintegration

package judge

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
	"github.com/rushikeshg25/cjudge/internal/sandbox"
)

func TestRealSandboxVerdicts(t *testing.T) {
	if os.Getenv("CJUDGE_TEST_DOCKER") != "1" {
		t.Skip("set CJUDGE_TEST_DOCKER=1 with built language images")
	}
	runtime := os.Getenv("CJUDGE_TEST_RUNTIME")
	if runtime == "" {
		runtime = "runsc"
	}
	d := &sandbox.Docker{Binary: "docker", Runtime: runtime}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := d.Check(ctx, []string{"cjudge-cpp:1", "cjudge-python:1", "cjudge-go:1"}); err != nil {
		t.Fatal(err)
	}
	// A /tmp path is visible to the host daemon on Linux and Docker Desktop.
	workspace, err := os.MkdirTemp("/tmp", "cjudge-integration-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)
	j := &Judge{Runner: d, Languages: Languages("cjudge-cpp:1", "cjudge-python:1", "cjudge-go:1"), Workspace: workspace}
	for _, tc := range []struct {
		name, lang, source         string
		want                       domain.Verdict
		timeMS, memoryMB, outputKB int
	}{
		{"python accepted", "python3", "a,b=map(int,input().split());print(a+b)", domain.Accepted, 3000, 128, 64},
		{"cpp accepted", "cpp20", "#include <iostream>\nint main(){int a,b;std::cin>>a>>b;std::cout<<a+b;}", domain.Accepted, 3000, 128, 64},
		{"go accepted", "go", "package main\nimport \"fmt\"\nfunc main(){var a,b int;fmt.Scan(&a,&b);fmt.Println(a+b)}", domain.Accepted, 3000, 128, 64},
		{"wrong answer", "python3", "print(99)", domain.WrongAnswer, 3000, 128, 64},
		{"compile error", "cpp20", "this is not c++", domain.CompileError, 3000, 128, 64},
		{"python syntax", "python3", "if broken syntax", domain.CompileError, 3000, 128, 64},
		{"python return outside function", "python3", "return 1", domain.CompileError, 3000, 128, 64},
		{"python break outside loop", "python3", "break", domain.CompileError, 3000, 128, 64},
		{"runtime error", "python3", "raise RuntimeError('private')", domain.RuntimeError, 3000, 128, 64},
		{"time limit", "python3", "while True: pass", domain.TimeLimit, 300, 128, 64},
		{"output limit", "python3", "print('x'*100000)", domain.OutputLimit, 3000, 128, 1},
		{"memory limit", "python3", "x=[]\nwhile True: x.append(bytearray(1024*1024))", domain.MemoryLimit, 10000, 32, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Problem{Title: "sum", Checker: "tokens", Limits: domain.Limits{TimeMS: tc.timeMS, MemoryMB: tc.memoryMB, OutputKB: tc.outputKB}, Tests: []domain.TestCase{{Input: "2 3\n", Expected: "5"}}}
			result, err := j.Evaluate(ctx, domain.Submission{Language: tc.lang, Source: tc.source}, p)
			if err != nil || result.Verdict != tc.want {
				t.Fatalf("got %+v err=%v, want %s", result, err, tc.want)
			}
		})
	}
}
