package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogger_callsInnerHandler(t *testing.T) {
	called := false
	h := Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", http.NoBody)
	h.ServeHTTP(w, r)

	if !called {
		t.Error("inner handler was not called")
	}
	if w.Code != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", w.Code)
	}
}

func TestResponseWriter_WriteHeader_idempotent(t *testing.T) {
	inner := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: inner, status: http.StatusOK}
	rw.WriteHeader(http.StatusCreated)
	rw.WriteHeader(http.StatusNotFound) // second call must be ignored
	if rw.status != http.StatusCreated {
		t.Errorf("status: got %d, want 201", rw.status)
	}
	if inner.Code != http.StatusCreated {
		t.Errorf("inner status: got %d, want 201", inner.Code)
	}
}

func TestResponseWriter_Write_impliesStatus200(t *testing.T) {
	inner := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: inner, status: http.StatusOK}
	if _, err := rw.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !rw.written {
		t.Error("written flag should be true after Write")
	}
	if rw.status != http.StatusOK {
		t.Errorf("status: got %d, want 200", rw.status)
	}
}
