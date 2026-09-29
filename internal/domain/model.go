package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrQueueFull = errors.New("queue full")
	ErrLeaseLost = errors.New("lease lost")
)

type Verdict string

const (
	Accepted     Verdict = "accepted"
	WrongAnswer  Verdict = "wrong_answer"
	CompileError Verdict = "compile_error"
	RuntimeError Verdict = "runtime_error"
	TimeLimit    Verdict = "time_limit_exceeded"
	MemoryLimit  Verdict = "memory_limit_exceeded"
	OutputLimit  Verdict = "output_limit_exceeded"
	SystemError  Verdict = "system_error"
)

type Limits struct {
	TimeMS   int `json:"time_ms"`
	MemoryMB int `json:"memory_mb"`
	OutputKB int `json:"output_kb"`
}

type TestCase struct {
	Input    string `json:"input"`
	Expected string `json:"expected"`
}

type Problem struct {
	AuthorID  string     `json:"-"`
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Statement string     `json:"statement"`
	Checker   string     `json:"checker"`
	Limits    Limits     `json:"limits"`
	Tests     []TestCase `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
}

func (p Problem) Validate() error {
	if strings.TrimSpace(p.Title) == "" || len(p.Title) > 200 || !utf8.ValidString(p.Title) {
		return errors.New("title must be 1..200 UTF-8 bytes")
	}
	if len(p.Statement) > 65536 || !utf8.ValidString(p.Statement) {
		return errors.New("statement must be at most 65536 UTF-8 bytes")
	}
	if p.Checker != "tokens" && p.Checker != "exact" {
		return errors.New("checker must be tokens or exact")
	}
	if p.Limits.TimeMS < 100 || p.Limits.TimeMS > 10000 {
		return errors.New("time_ms must be 100..10000")
	}
	if p.Limits.MemoryMB < 32 || p.Limits.MemoryMB > 512 {
		return errors.New("memory_mb must be 32..512")
	}
	if p.Limits.OutputKB < 1 || p.Limits.OutputKB > 1024 {
		return errors.New("output_kb must be 1..1024")
	}
	if len(p.Tests) < 1 || len(p.Tests) > 100 {
		return errors.New("test count must be 1..100")
	}
	total := 0
	for _, t := range p.Tests {
		if !utf8.ValidString(t.Input) || !utf8.ValidString(t.Expected) || strings.ContainsRune(t.Input, 0) || strings.ContainsRune(t.Expected, 0) {
			return errors.New("tests must contain UTF-8 text without NUL")
		}
		if len(t.Input) > 1<<20 || len(t.Expected) > 1<<20 {
			return errors.New("test input/output must be at most 1 MiB each")
		}
		total += len(t.Input) + len(t.Expected)
	}
	if total > 4<<20 {
		return errors.New("test set must be at most 4 MiB")
	}
	return nil
}

type SubmissionRequest struct {
	ProblemID string `json:"problem_id"`
	Language  string `json:"language"`
	Source    string `json:"source"`
}

func (s SubmissionRequest) Validate() error {
	if !ValidID(s.ProblemID) {
		return errors.New("invalid problem_id")
	}
	if s.Language != "cpp20" && s.Language != "python3" && s.Language != "go" {
		return errors.New("language must be cpp20, python3, or go")
	}
	if len(s.Source) < 1 || len(s.Source) > 65536 || !utf8.ValidString(s.Source) || strings.ContainsRune(s.Source, 0) {
		return errors.New("source must be 1..65536 UTF-8 bytes without NUL")
	}
	return nil
}

type Result struct {
	Verdict    Verdict `json:"verdict"`
	Passed     int     `json:"passed"`
	Total      int     `json:"total"`
	TimeMS     int64   `json:"time_ms"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

type Submission struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"-"`
	ProblemID string    `json:"problem_id"`
	Language  string    `json:"language"`
	Source    string    `json:"-"`
	State     string    `json:"state"`
	Attempts  int       `json:"attempts"`
	Result    *Result   `json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Job struct {
	Submission
	LeaseToken string
}

type Principal struct {
	ID    string
	Admin bool
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("secure random: %v", err))
	}
	return hex.EncodeToString(b[:])
}

func ValidID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && id == strings.ToLower(id)
}
