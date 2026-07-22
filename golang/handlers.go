package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Server holds route handler deps. Clientset injected for testability.
type Server struct {
	clientset kubernetes.Interface

	// A field, not a direct call, so tests can fake an unreachable API — client-go's fake discovery ignores reactor errors on ServerVersion().
	getK8sVersion func(kubernetes.Interface) (string, error)

	// A per-Server registry, not prometheus.DefaultRegisterer — that's process-global and MustRegister
	// panics on a second registration, which every test constructing its own Server would trigger.
	registry           *prometheus.Registry
	healthzChecksTotal *prometheus.CounterVec
}

// NewServer constructs a Server backed by the given Kubernetes clientset.
func NewServer(clientset kubernetes.Interface) *Server {
	registry := prometheus.NewRegistry()

	healthzChecksTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tyk_healthz_checks_total",
			Help: "Total /healthz checks, by result.",
		},
		[]string{"status"},
	)
	registry.MustRegister(healthzChecksTotal)
	registry.MustRegister(&deploymentCollector{clientset: clientset})

	return &Server{
		clientset:          clientset,
		getK8sVersion:      getKubernetesVersion,
		registry:           registry,
		healthzChecksTotal: healthzChecksTotal,
	}
}

// Start registers routes and serves on listenAddr until the server fails or
// ctx is cancelled (SIGTERM/SIGINT), at which point it drains in-flight
// requests before returning.
func (s *Server) Start(ctx context.Context, listenAddr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.indexHandler)
	mux.HandleFunc("/healthz", s.healthHandler)
	mux.HandleFunc("/deployments", s.deploymentsHandler)
	mux.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: mux,
		// Bound slow clients the same way we bound slow upstreams: WriteTimeout
		// exceeds the 10s K8s handler timeout so it can never cut a valid
		// response short; ReadHeaderTimeout closes slowloris-style connections.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", listenAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		// ListenAndServe never returns nil — this is a bind or serve failure.
		return err
	case <-ctx.Done():
	}

	// Drain in-flight requests. 15s exceeds the 10s handler timeout, so no
	// request that was going to succeed gets cut off by the shutdown itself.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	slog.Info("shutting down, draining in-flight requests")
	return srv.Shutdown(shutdownCtx)
}

// writeJSON encodes payload as the JSON response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed writing json response", "err", err)
	}
}

// healthHandler reports live K8s API connectivity, replacing the old static "ok" response.
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	version, err := s.getK8sVersion(s.clientset)
	if err != nil {
		s.healthzChecksTotal.WithLabelValues("error").Inc()
		writeJSON(w, http.StatusServiceUnavailable, HealthResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	s.healthzChecksTotal.WithLabelValues("ok").Inc()
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:            "ok",
		KubernetesVersion: version,
	})
}

// deploymentsHandler reports each Deployment's ready-vs-desired replicas (optionally filtered by ?namespace=); overall healthy only if all are.
func (s *Server) deploymentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Message: "method not allowed"})
		return
	}

	namespace := r.URL.Query().Get("namespace")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	deployments, err := s.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Message: fmt.Sprintf("failed to list deployments: %v", err)})
		return
	}

	resp := DeploymentsResponse{Healthy: true, Deployments: []DeploymentHealth{}}

	for _, d := range deployments.Items {
		desired, ready := deploymentReplicas(d.Spec.Replicas, d.Status.ReadyReplicas)
		healthy := ready == desired
		if !healthy {
			resp.Healthy = false
		}

		resp.Deployments = append(resp.Deployments, DeploymentHealth{
			Name:            d.Name,
			Namespace:       d.Namespace,
			DesiredReplicas: desired,
			ReadyReplicas:   ready,
			Healthy:         healthy,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// deploymentReplicas applies the K8s API convention that nil replicas defaults to 1 — shared by
// deploymentsHandler and deploymentCollector so the two never disagree on what "desired" means.
func deploymentReplicas(specReplicas *int32, readyReplicas int32) (desired, ready int32) {
	desired = 1
	if specReplicas != nil {
		desired = *specReplicas
	}
	return desired, readyReplicas
}
