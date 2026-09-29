package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

type fakeRepository struct {
	Repository
	admin      bool
	deny       bool
	authErr    error
	enqueueErr error
	created    bool
	owner      string
}

func (f *fakeRepository) Ping(context.Context) error { return nil }
func (f *fakeRepository) Authenticate(context.Context, string) (domain.Principal, error) {
	return domain.Principal{ID: "owner", Admin: f.admin}, f.authErr
}
func (f *fakeRepository) Allow(context.Context, string, int) (bool, error) { return !f.deny, nil }
func (f *fakeRepository) CreateProblem(_ context.Context, p domain.Problem) (domain.Problem, error) {
	p.ID = domain.NewID()
	return p, nil
}
func (f *fakeRepository) Enqueue(_ context.Context, owner, key string, r domain.SubmissionRequest, capacity int) (domain.Submission, bool, error) {
	f.owner = owner
	return domain.Submission{ID: domain.NewID(), Source: r.Source, OwnerID: owner, State: "queued"}, f.created, f.enqueueErr
}
func (f *fakeRepository) Submission(_ context.Context, id, owner string) (domain.Submission, error) {
	f.owner = owner
	return domain.Submission{}, domain.ErrNotFound
}

func handler(repo *fakeRepository) http.Handler {
	return (&Server{Repo: repo, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), QueueLimit: 10, RequestsPerMinute: 10}).Handler()
}
func request(h http.Handler, method, path, body, token, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthAndRequestBoundaries(t *testing.T) {
	valid := `{"problem_id":"` + domain.NewID() + `","language":"go","source":"private source"}`
	for _, tc := range []struct {
		name, body, token, key string
		repo                   fakeRepository
		want                   int
	}{
		{"missing_auth", valid, "", "k", fakeRepository{}, 401},
		{"bad_auth", valid, "token", "k", fakeRepository{authErr: domain.ErrNotFound}, 401},
		{"missing_key", valid, "token", "", fakeRepository{}, 400},
		{"unknown_field", `{"extra":1}`, "token", "k", fakeRepository{}, 400},
		{"two_json", valid + valid, "token", "k", fakeRepository{}, 400},
		{"unsupported_language", strings.Replace(valid, `"go"`, `"bash"`, 1), "token", "k", fakeRepository{}, 422},
		{"oversized", `{"source":"` + strings.Repeat("a", 410<<10) + `"}`, "token", "k", fakeRepository{}, 413},
		{"rate_limit", valid, "token", "k", fakeRepository{deny: true}, 429},
		{"conflict", valid, "token", "k", fakeRepository{enqueueErr: domain.ErrConflict}, 409},
		{"full", valid, "token", "k", fakeRepository{enqueueErr: domain.ErrQueueFull}, 503},
		{"new", valid, "token", "k", fakeRepository{created: true}, 202},
		{"replay", valid, "token", "k", fakeRepository{}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(handler(&tc.repo), "POST", "/v1/submissions", tc.body, tc.token, tc.key)
			if w.Code != tc.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private source") {
				t.Fatal("source disclosed")
			}
			if w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request id")
			}
		})
	}
}

func TestProblemAuthorizationAndTestPrivacy(t *testing.T) {
	body := `{"title":"sum","statement":"add","checker":"tokens","limits":{"time_ms":1000,"memory_mb":128,"output_kb":64},"tests":[{"input":"hidden input","expected":"hidden answer"}]}`
	w := request(handler(&fakeRepository{}), "POST", "/v1/problems", body, "token", "")
	if w.Code != 403 {
		t.Fatalf("non-admin published problem: %d", w.Code)
	}
	w = request(handler(&fakeRepository{admin: true}), "POST", "/v1/problems", body, "token", "")
	if w.Code != 201 {
		t.Fatalf("publish failed: %s", w.Body.String())
	}
	for _, secret := range []string{"hidden input", "hidden answer", "tests"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("test data leaked: %s", w.Body.String())
		}
	}
}

func TestOwnedLookup(t *testing.T) {
	repo := &fakeRepository{}
	w := request(handler(repo), "GET", "/v1/submissions/"+domain.NewID(), "", "token", "")
	if w.Code != 404 || repo.owner != "owner" {
		t.Fatal("lookup did not enforce principal")
	}
}

func TestPaginationAndMediaType(t *testing.T) {
	w := request(handler(&fakeRepository{}), "GET", "/v1/submissions?limit=1000", "", "token", "")
	if w.Code != 400 {
		t.Fatal("unbounded page accepted")
	}
	r := httptest.NewRequest("POST", "/v1/submissions", strings.NewReader("{}"))
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Idempotency-Key", "k")
	w = httptest.NewRecorder()
	handler(&fakeRepository{}).ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("untyped body accepted")
	}
	var envelope map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope["error"] == nil {
		t.Fatal("missing error envelope")
	}
}
