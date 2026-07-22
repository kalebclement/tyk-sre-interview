package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// --- / (dashboard) ---

func TestIndexHandler_ServesDashboard(t *testing.T) {
	s := NewServer(fake.NewSimpleClientset())
	rec := httptest.NewRecorder()
	s.indexHandler(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "default-src 'none'")
	// Sanity-check the embedded asset actually made it into the binary.
	assert.Contains(t, rec.Body.String(), "tyk-sre-tool")
}

func TestIndexHandler_UnknownPathIs404(t *testing.T) {
	// "/" is the mux catch-all, so unknown routes land on this handler too.
	s := NewServer(fake.NewSimpleClientset())
	rec := httptest.NewRecorder()
	s.indexHandler(rec, httptest.NewRequest(http.MethodGet, "/no-such-route", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "not found", resp.Message)
}

func TestIndexHandler_MethodNotAllowed(t *testing.T) {
	s := NewServer(fake.NewSimpleClientset())
	rec := httptest.NewRecorder()
	s.indexHandler(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
