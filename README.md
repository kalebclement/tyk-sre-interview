# tyk-sre-assignment

A lightweight SRE tool that monitors Kubernetes Deployment health and API server connectivity, built as a single Go binary.

## What it does

- **`GET /healthz`** — verifies live connectivity to the K8s API server, returns the cluster version. Returns `503` when the API server is unreachable instead of crashing.
- **`GET /deployments`** — compares `spec.replicas` vs `status.readyReplicas` for every Deployment in the cluster. Supports `?namespace=` filtering. Returns an overall health status.
- **`GET /metrics`** — Prometheus-format metrics: `tyk_deployment_desired_replicas` and `tyk_deployment_ready_replicas` gauges per deployment, plus `tyk_healthz_checks_total` counter by result.

## Build & run

```bash
cd golang
go mod tidy && go build -o tyk-sre-assignment .

# Against a real cluster
./tyk-sre-assignment --kubeconfig ~/.kube/config --address :8080

# Quick check
curl http://localhost:8080/healthz
curl http://localhost:8080/deployments
curl http://localhost:8080/deployments?namespace=default
```

## Tests

```bash
cd golang
go test -v ./...
```

All tests use `fake.NewSimpleClientset()` — no real cluster required.

## Docker

Multi-stage build, runs on `distroless/static-debian12:nonroot` (~10 MB image).

```bash
cd golang
docker build -t tyk-sre-assignment .
```

## Helm

```bash
cd golang
helm install tyk-sre-tool helm/tyk-sre-tool -n monitoring --create-namespace

# Verify
kubectl port-forward -n monitoring svc/tyk-sre-tool 8080:80
curl http://localhost:8080/healthz
```

Key config in `values.yaml`:
- `rbac.create` — toggles ClusterRole (read-only: get/list/watch on Deployments)
- `serviceMonitor.enabled` — creates a Prometheus ServiceMonitor if the Operator CRDs are present
- `replicaCount`, `resources`, `image.tag` — standard knobs

## CI/CD

Two GitHub Actions workflows:

- **`ci.yaml`** (on PR to `main`) — runs `go test`, `go vet`, `gofmt` check, builds the Docker image, and smoke-tests it by booting against an unreachable API server and asserting `/healthz` returns 503 instead of crashing.
- **`release.yaml`** (on merge to `main`) — re-runs tests, builds the image, tags with git SHA + `latest`, pushes to GHCR.

The split ensures untested code never produces a published image, and `main` stays deployable.

## Project structure

```
golang/
├── main.go              # entrypoint, flag parsing, K8s client setup
├── handlers.go          # HTTP handlers, Server struct with injected clientset
├── types.go             # request/response JSON structs
├── metrics.go           # Prometheus collector for deployment health gauges
├── main_test.go         # getKubernetesVersion unit test
├── handlers_test.go     # handler tests (healthz, deployments, error cases)
├── metrics_test.go      # collector tests
├── Dockerfile           # multi-stage: golang:1.23-alpine → distroless
├── .dockerignore
└── helm/
    └── tyk-sre-tool/    # Helm chart
        ├── Chart.yaml
        ├── values.yaml
        └── templates/
```

## Design decisions

- **Dependency injection** — the `Server` struct takes a `kubernetes.Interface`, so tests swap in a fake clientset with no cluster needed.
- **Per-server Prometheus registry** — avoids `MustRegister` panics when multiple tests construct their own `Server`.
- **Stateless** — no persistent storage; reads cluster state on demand. Restarts lose nothing.
- **Separate liveness/readiness tuning** — readiness probe fails fast (3 attempts) so the pod leaves the Service quickly; liveness probe is lenient (12 attempts) because restarting can't fix an API server outage.