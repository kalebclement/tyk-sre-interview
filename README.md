# tyk-sre-assignment

A lightweight Kubernetes Deployment health monitor built as a single Go binary. Exposes HTTP endpoints for liveness checks, per-Deployment replica status, and Prometheus metrics — designed to run in-cluster alongside your existing observability stack.

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  Kubernetes cluster                                              │
│                                                                  │
│   ┌───────────────────┐          ┌────────────────┐              │
│   │  tyk-sre-tool     │─────────▶│  K8s API       │              │
│   │  (Deployment)     │   List   │  Server        │              │
│   │                   │ Deploys  │                │              │
│   │  GET /healthz     │◀─────────│  ServerVersion │              │
│   │  GET /deployments │          └────────────────┘              │
│   │  GET /metrics     │                                          │
│   └────────┬──────────┘                                          │
│            │ :8080                                               │
│   ┌────────┴──────────┐          ┌────────────────┐              │
│   │ Service (ClusterIP)│◀────────│ ServiceMonitor │              │
│   └───────────────────┘          │  (optional)    │              │
│                                  └───────┬────────┘              │
│                                          │                       │
│                                  ┌───────▼────────┐              │
│                                  │  Prometheus    │              │
│                                  └────────────────┘              │
└──────────────────────────────────────────────────────────────────┘
```

The tool uses a read-only `ClusterRole` (`get`, `list`, `watch` on Deployments). Every Kubernetes API call is bounded: handler and collector calls carry a 10s `context.WithTimeout`, and a 15s client-level timeout on the REST config backstops calls that take no context (like `ServerVersion()`). It runs as a distroless nonroot container (~10 MB).

## Endpoints

| Endpoint | Method | Description |
|---|---|---|
| `/` | GET | Embedded status dashboard — live HTML view of health + deployments, auto-refreshes every 5s. |
| `/healthz` | GET | Verifies live K8s API connectivity; returns cluster version. 503 when unreachable. |
| `/deployments` | GET | Compares `spec.replicas` vs `status.readyReplicas` for every Deployment. Supports `?namespace=` filtering. |
| `/metrics` | GET | Prometheus-format metrics (see below). |

### Dashboard

A dependency-free HTML dashboard is compiled into the binary via `go:embed` and served at `/`. It renders `/healthz` and `/deployments` client-side, auto-refreshes every 5 seconds, supports namespace filtering, and sorts degraded deployments to the top so problems surface first. It pauses polling in background tabs to avoid needless K8s API load, and ships with a strict inline-only Content-Security-Policy — the page cannot load or call anything external.

No npm, no build step, no new supply-chain surface: the distroless image and `readOnlyRootFilesystem` are unaffected. `/metrics` stays raw Prometheus format; point Grafana at it for history.

### Example responses

```json
// GET /healthz — healthy
{"status":"ok","kubernetes_version":"1.28.0"}

// GET /healthz — API server unreachable
{"status":"error","message":"connection refused"}

// GET /deployments?namespace=default
{
  "healthy": true,
  "deployments": [
    {"name":"api","namespace":"default","desiredReplicas":3,"readyReplicas":3,"healthy":true}
  ]
}
```

### Prometheus metrics

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `tyk_deployment_desired_replicas` | Gauge | `namespace`, `deployment` | Desired replicas per Deployment |
| `tyk_deployment_ready_replicas` | Gauge | `namespace`, `deployment` | Ready replicas per Deployment |
| `tyk_deployment_scrape_success` | Gauge | — | 1 if the last K8s API scrape succeeded, 0 if not — alert on `== 0` |
| `tyk_healthz_checks_total` | Counter | `status` (`ok` / `error`) | Health check outcomes |

Unhealthy deployments aren't exposed as a separate metric; alerting rules derive it as `desired != ready`. Scrape failures are reported explicitly via `tyk_deployment_scrape_success` rather than silently contributing nothing.

## Quick start

### Local (outside cluster)

```bash
cd golang
make build
./tyk-sre-assignment --kubeconfig ~/.kube/config --address :8080

curl http://localhost:8080/healthz
curl http://localhost:8080/deployments
curl http://localhost:8080/deployments?namespace=default
```

Then open `http://localhost:8080/` in a browser for the dashboard.

### Docker

```bash
cd golang
make docker-build
make docker-run   # mounts ~/.kube/config automatically
```

### Helm (in-cluster)

```bash
cd golang

# Default (development)
helm install tyk-sre-tool helm/tyk-sre-tool -n monitoring --create-namespace

# Staging — lighter resources, debug-friendly
helm install tyk-sre-tool helm/tyk-sre-tool -n monitoring \
  -f helm/tyk-sre-tool/values-staging.yaml

# Production — HA, PDB, NetworkPolicy, ServiceMonitor
helm install tyk-sre-tool helm/tyk-sre-tool -n monitoring \
  -f helm/tyk-sre-tool/values-production.yaml

# Verify
kubectl port-forward -n monitoring svc/tyk-sre-tool 8080:80
curl http://localhost:8080/healthz
```

### Makefile targets

```
make build         Compile the Go binary
make test          Run unit tests with -race
make vet           go vet static analysis
make fmt           Check gofmt; fail if unformatted
make lint          vet + fmt in one pass
make docker-build  Build the container image
make docker-run    Build and run with local kubeconfig
make clean         Remove the compiled binary
make help          Show available targets
```

## Tests

All tests use `fake.NewSimpleClientset()` — no real cluster required.

```bash
cd golang
make test
```

Coverage includes handler responses (healthy, unhealthy, namespace filter, API errors, method-not-allowed), the dashboard handler (served page, JSON 404 catch-all, method-not-allowed), the Prometheus collector including its error path, and `getKubernetesVersion`.

## CI/CD pipeline

Two GitHub Actions workflows enforce a strict gate: untested code never produces a published image.

### `ci.yaml` — PR quality gate

Three jobs run on every pull request to `main`:

**Test & vet:** `go build`, `go vet`, `gofmt` check, `go test -race` (matching `make test`), then `govulncheck` (pinned v1.5.0) for source-level vulnerability scanning.

**Docker build & smoke test:** builds the image, scans it with Trivy (fails on CRITICAL/HIGH), then boots the container against an unreachable API server and asserts `/healthz` returns 503 instead of crashing.

**Helm lint & validate:** `helm lint` plus `kubeconform` validation of rendered manifests for all three values files — production matters most, since it's the only one rendering the PDB, NetworkPolicy, and ServiceMonitor.

### `release.yaml` — publish on merge

1. Re-runs tests (defense in depth)
2. Builds and pushes to GHCR, tagged with the git SHA, `latest`, and the chart's `appVersion` — read from `Chart.yaml` at build time so the chart's default image tag always exists in the registry
3. Signs the image with cosign keyless (Sigstore OIDC)

### Supply-chain hardening

- All GitHub Actions and Dockerfile base images are **SHA-pinned** (no mutable tags)
- **Dependabot** watches Go modules, GitHub Actions, and Docker base images weekly
- **cosign keyless signing** on every release — consumers can verify provenance
- **Trivy** gates CI on CRITICAL/HIGH CVEs in the final image

## Helm chart

The chart (`golang/helm/tyk-sre-tool/`) ships with three values files:

| File | PDB | NetworkPolicy | ServiceMonitor | Use case |
|---|---|---|---|---|
| `values.yaml` | off | off | off | Development / quick install |
| `values-staging.yaml` | off | off | off | Pre-production, lighter resources |
| `values-production.yaml` | on (minAvailable: 1) | on | on | Production deployment |

Key configuration:

- `rbac.create` — ClusterRole for read-only Deployment access
- `metrics.serviceMonitor.enabled` — creates a Prometheus ServiceMonitor; the chart checks for the Prometheus Operator CRD and fails loudly if it's missing rather than silently deploying a broken resource
- `podDisruptionBudget.enabled` — prevents voluntary eviction from killing all replicas
- `networkPolicy.enabled` — restricts ingress to allowed namespaces
- Pod security: `runAsNonRoot`, `readOnlyRootFilesystem`, all capabilities dropped, seccomp `RuntimeDefault`

## Project structure

```
.
├── .github/
│   ├── dependabot.yml              # Weekly: Go modules, Actions, Docker
│   └── workflows/
│       ├── ci.yaml                 # PR gate: test/vet/fmt, govulncheck, trivy, smoke test, helm validation
│       └── release.yaml            # Merge to main: test, build, push GHCR, cosign sign
├── golang/
│   ├── main.go                     # Entrypoint, flags, K8s client setup (15s client timeout)
│   ├── handlers.go                 # HTTP handlers, Server struct with injected clientset
│   ├── ui.go                       # Embedded dashboard handler (go:embed, CSP, JSON 404 catch-all)
│   ├── types.go                    # Request/response JSON structs
│   ├── metrics.go                  # Prometheus collector for deployment health gauges
│   ├── main_test.go
│   ├── handlers_test.go
│   ├── ui_test.go
│   ├── metrics_test.go
│   ├── web/
│   │   └── index.html              # Dashboard page — dependency-free HTML/JS, compiled into the binary
│   ├── Makefile                    # Build, test, lint, docker targets
│   ├── Dockerfile                  # Multi-stage: golang:1.26-alpine → distroless nonroot
│   ├── .dockerignore
│   └── helm/
│       └── tyk-sre-tool/
│           ├── Chart.yaml
│           ├── values.yaml
│           ├── values-staging.yaml
│           ├── values-production.yaml
│           └── templates/
├── LICENSE.md
└── README.md
```

## Design decisions

- **Dependency injection** — `Server` takes a `kubernetes.Interface`, so tests swap in a fake clientset with no cluster needed.
- **Per-server Prometheus registry** — avoids `MustRegister` panics when multiple tests construct their own `Server`.
- **Bounded API calls at two levels** — 10s context timeouts on List calls (derived from `r.Context()` in handlers, so client disconnects still cancel early), with a 15s client-level timeout as the backstop for calls that take no context, like `ServerVersion()`. The context timeouts are deliberately shorter so they fire first where they exist.
- **Explicit scrape failure signal** — the collector reports `tyk_deployment_scrape_success 0` on API errors instead of silently contributing nothing, keeping `/metrics` serving (unlike failing the whole endpoint) while making failures alertable.
- **Embedded zero-dependency dashboard** — a single HTML file compiled in via `go:embed`, served with a strict inline-only CSP. A pure consumer of the existing JSON endpoints, so it can never disagree with the API; no npm or build stage enters the supply chain, and the distroless image is unchanged.
- **Structured logging** — `log/slog` with a JSON handler; machine-parseable by any log pipeline, with levels that mean something (startup API unreachability is a warning, not fatal), and config errors exit cleanly instead of panicking with a stack trace.
- **Stateless** — no persistent storage; reads cluster state on demand. Restarts lose nothing.
- **Separate liveness/readiness tuning** — readiness fails fast (3 attempts) so the pod leaves the Service quickly; liveness is lenient (12 attempts) because restarting can't fix an API server outage.