package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/runner"
)

type stubSync struct {
	result gitops.Result
	err    error
	allErr error
	called string
}

func (s *stubSync) SyncOne(_ context.Context, name string) (gitops.Result, error) {
	s.called = name
	if s.err != nil {
		return gitops.Result{}, s.err
	}
	result := s.result
	result.Name = name
	return result, nil
}

func (s *stubSync) StartSyncAll(_ context.Context) error {
	return s.allErr
}

func TestSyncRepositoryReturnsResult(t *testing.T) {
	t.Parallel()

	trigger := &stubSync{result: gitops.Result{Action: "updated", Branch: "main"}}
	handler := newTestRouterWithSync(t, trigger)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, "/v1/repos/example/sync"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if trigger.called != "example" {
		t.Fatalf("called = %q", trigger.called)
	}

	var payload gitops.Result
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Action != "updated" {
		t.Fatalf("action = %q", payload.Action)
	}
}

func TestSyncRepositoryReportsUnknownRepository(t *testing.T) {
	t.Parallel()

	trigger := &stubSync{err: runner.ErrRepoNotManaged}
	handler := newTestRouterWithSync(t, trigger)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, "/v1/repos/nope/sync"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestSyncRepositoryReportsBusy(t *testing.T) {
	t.Parallel()

	trigger := &stubSync{err: runner.ErrSyncBusy}
	handler := newTestRouterWithSync(t, trigger)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, "/v1/repos/example/sync"))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
}

func TestSyncAllAcceptsRequest(t *testing.T) {
	t.Parallel()

	trigger := &stubSync{}
	handler := newTestRouterWithSync(t, trigger)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorizedRequest(http.MethodPost, "/v1/sync"))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestSyncEndpointsRequireKey(t *testing.T) {
	t.Parallel()

	handler := newTestRouterWithSync(t, &stubSync{})

	for _, target := range []string{"/v1/sync", "/v1/repos/example/sync"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, target, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", target, recorder.Code)
		}
	}
}
