package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestDeploymentCollector_Collect(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		deployment("default", "api", 3, 3),
		deployment("kube-system", "coredns", 1, 0),
	)

	collector := &deploymentCollector{clientset: clientset}

	expected := `
# HELP tyk_deployment_desired_replicas Desired replica count for a Deployment.
# TYPE tyk_deployment_desired_replicas gauge
tyk_deployment_desired_replicas{deployment="api",namespace="default"} 3
tyk_deployment_desired_replicas{deployment="coredns",namespace="kube-system"} 1
# HELP tyk_deployment_ready_replicas Ready replica count for a Deployment.
# TYPE tyk_deployment_ready_replicas gauge
tyk_deployment_ready_replicas{deployment="api",namespace="default"} 3
tyk_deployment_ready_replicas{deployment="coredns",namespace="kube-system"} 0
`
	assert.NoError(t, testutil.CollectAndCompare(collector, strings.NewReader(expected)))
}

func TestDeploymentCollector_ListError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("list", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api server unavailable")
	})

	collector := &deploymentCollector{clientset: clientset}

	// A failed live query just contributes zero metrics for this scrape, not an error/panic.
	assert.NoError(t, testutil.CollectAndCompare(collector, strings.NewReader("")))
}

func TestHealthzChecksTotal_CountsOkAndError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	s := NewServer(clientset)

	s.getK8sVersion = func(kubernetes.Interface) (string, error) { return "1.28.0-fake", nil }
	s.healthHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	s.healthHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, float64(2), testutil.ToFloat64(s.healthzChecksTotal.WithLabelValues("ok")))

	s.getK8sVersion = func(kubernetes.Interface) (string, error) { return "", errors.New("connection refused") }
	s.healthHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, float64(1), testutil.ToFloat64(s.healthzChecksTotal.WithLabelValues("error")))
}

func TestMetricsHandler_ServesRegistry(t *testing.T) {
	s := NewServer(fake.NewSimpleClientset(deployment("default", "api", 2, 2)))
	s.healthHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	rec := httptest.NewRecorder()
	promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `tyk_deployment_desired_replicas{deployment="api",namespace="default"} 2`)
	assert.Contains(t, body, "tyk_healthz_checks_total")
}
