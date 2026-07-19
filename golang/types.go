package main

// HealthResponse is returned by GET /healthz.
type HealthResponse struct {
	Status            string `json:"status"`
	KubernetesVersion string `json:"kubernetes_version,omitempty"`
	Message           string `json:"message,omitempty"`
}

// ErrorResponse is a generic JSON error payload returned by any handler.
type ErrorResponse struct {
	Message string `json:"message"`
}

// DeploymentHealth compares a Deployment's ready replicas against its desired spec.
type DeploymentHealth struct {
	Name            string `json:"name"`
	Namespace       string `json:"namespace"`
	DesiredReplicas int32  `json:"desiredReplicas"`
	ReadyReplicas   int32  `json:"readyReplicas"`
	Healthy         bool   `json:"healthy"`
}

// DeploymentsResponse is returned by GET /deployments.
type DeploymentsResponse struct {
	Healthy     bool               `json:"healthy"`
	Deployments []DeploymentHealth `json:"deployments"`
}
