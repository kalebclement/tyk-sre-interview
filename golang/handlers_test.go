package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	disco "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func int32Ptr(i int32) *int32 { return &i }

// --- /healthz ---

func TestHealthHandler_Healthy(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.Discovery().(*disco.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "1.28.0-fake"}

	s := NewServer(clientset)
	rec := httptest.NewRecorder()
	s.healthHandler(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, "1.28.0-fake", resp.KubernetesVersion)
}

func TestHealthHandler_Unreachable(t *testing.T) {
	// Fake discovery ignores reactor errors on ServerVersion(), so stub getK8sVersion directly instead.
	s := NewServer(fake.NewSimpleClientset())
	s.getK8sVersion = func(kubernetes.Interface) (string, error) {
		return "", errors.New("connection refused")
	}

	rec := httptest.NewRecorder()
	s.healthHandler(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp.Status)
	assert.Contains(t, resp.Message, "connection refused")
}

// --- /deployments ---

func deployment(ns, name string, desired, ready int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(desired)},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: ready},
	}
}

func TestDeploymentsHandler_AllHealthy(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		deployment("default", "api", 3, 3),
		deployment("default", "worker", 1, 1),
	)

	s := NewServer(clientset)
	rec := httptest.NewRecorder()
	s.deploymentsHandler(rec, httptest.NewRequest(http.MethodGet, "/deployments", nil))

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp DeploymentsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Healthy)
	assert.Len(t, resp.Deployments, 2)
}

func TestDeploymentsHandler_Unhealthy(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		deployment("default", "api", 3, 3),
		deployment("default", "worker", 2, 1),
	)

	s := NewServer(clientset)
	rec := httptest.NewRecorder()
	s.deploymentsHandler(rec, httptest.NewRequest(http.MethodGet, "/deployments", nil))

	var resp DeploymentsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Healthy)

	for _, d := range resp.Deployments {
		if d.Name == "worker" {
			assert.False(t, d.Healthy)
			assert.Equal(t, int32(2), d.DesiredReplicas)
			assert.Equal(t, int32(1), d.ReadyReplicas)
		}
	}
}

func TestDeploymentsHandler_NamespaceFilter(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		deployment("team-a", "api", 1, 1),
		deployment("team-b", "api", 1, 1),
	)

	s := NewServer(clientset)
	rec := httptest.NewRecorder()
	s.deploymentsHandler(rec, httptest.NewRequest(http.MethodGet, "/deployments?namespace=team-a", nil))

	var resp DeploymentsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Deployments, 1)
	assert.Equal(t, "team-a", resp.Deployments[0].Namespace)
}

func TestDeploymentsHandler_ListError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("list", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api server unavailable")
	})

	s := NewServer(clientset)
	rec := httptest.NewRecorder()
	s.deploymentsHandler(rec, httptest.NewRequest(http.MethodGet, "/deployments", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// --- Start / shutdown ---

func TestServerStart_GracefulShutdown(t *testing.T) {
	s := NewServer(fake.NewSimpleClientset())
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- s.Start(ctx, "127.0.0.1:0") }()

	// Give the listener a moment to come up, then trigger shutdown.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		// A clean drain returns nil, not http.ErrServerClosed.
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down after context cancellation")
	}
}