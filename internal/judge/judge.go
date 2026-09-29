package judge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rushikeshg25/cjudge/internal/domain"
	"github.com/rushikeshg25/cjudge/internal/sandbox"
)

type Judge struct {
	Runner    sandbox.Runner
	Languages map[string]Language
	Workspace string
}

func (j *Judge) Evaluate(ctx context.Context, sub domain.Submission, p domain.Problem) (domain.Result, error) {
	result := domain.Result{Total: len(p.Tests)}
	lang, ok := j.Languages[sub.Language]
	if !ok {
		return result, fmt.Errorf("language unavailable")
	}
	if err := p.Validate(); err != nil {
		return result, fmt.Errorf("invalid stored problem: %w", err)
	}
	if err := os.MkdirAll(j.Workspace, 0700); err != nil {
		return result, err
	}
	work, err := os.MkdirTemp(j.Workspace, "job-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(work)
	if err = os.Chmod(work, 0755); err != nil {
		return result, err
	}
	if err = os.WriteFile(filepath.Join(work, lang.SourceFile), []byte(sub.Source), 0444); err != nil {
		return result, err
	}
	if len(lang.Compile) > 0 {
		artifact := "/tmp/program"
		if sub.Language == "python3" {
			artifact = ""
		}
		out, err := j.Runner.Run(ctx, sandbox.Request{Image: lang.Image, Command: lang.Compile, Workspace: work, Timeout: 45 * time.Second, MemoryMB: 512, OutputBytes: 16384, ArtifactPath: artifact})
		if err != nil {
			return result, err
		}
		if out.ExitCode != 0 || out.TimedOut || out.OOM || out.OutputExceeded {
			result.Verdict = domain.CompileError
			result.Diagnostic = diagnostic(out.Stderr + out.Stdout)
			if result.Diagnostic == "" {
				result.Diagnostic = "compilation failed or exceeded compiler limits"
			}
			return result, nil
		}
	}
	for _, tc := range p.Tests {
		out, err := j.Runner.Run(ctx, sandbox.Request{Image: lang.Image, Command: lang.Run, Workspace: work, Input: tc.Input, Timeout: time.Duration(p.Limits.TimeMS) * time.Millisecond, MemoryMB: p.Limits.MemoryMB, OutputBytes: p.Limits.OutputKB * 1024})
		if err != nil {
			return result, err
		}
		result.TimeMS = max(result.TimeMS, out.Duration.Milliseconds())
		switch {
		case out.OOM:
			result.Verdict = domain.MemoryLimit
		case out.OutputExceeded:
			result.Verdict = domain.OutputLimit
		case out.TimedOut:
			result.Verdict = domain.TimeLimit
		case out.ExitCode != 0:
			result.Verdict = domain.RuntimeError
		case !matches(p.Checker, out.Stdout, tc.Expected):
			result.Verdict = domain.WrongAnswer
		default:
			result.Passed++
			continue
		}
		// Runtime stdout/stderr can reveal hidden input; never publish them.
		return result, nil
	}
	result.Verdict = domain.Accepted
	return result, nil
}

func matches(checker, actual, expected string) bool {
	if checker == "exact" {
		return actual == expected
	}
	a, b := strings.Fields(actual), strings.Fields(expected)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func diagnostic(s string) string {
	s = strings.ToValidUTF8(s, "?")
	if len(s) > 4096 {
		s = s[:4096]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
