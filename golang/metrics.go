package main

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	deploymentDesiredReplicasDesc = prometheus.NewDesc(
		"tyk_deployment_desired_replicas",
		"Desired replica count for a Deployment.",
		[]string{"namespace", "deployment"}, nil,
	)
	deploymentReadyReplicasDesc = prometheus.NewDesc(
		"tyk_deployment_ready_replicas",
		"Ready replica count for a Deployment.",
		[]string{"namespace", "deployment"}, nil,
	)
	deploymentScrapeSuccessDesc = prometheus.NewDesc(
		"tyk_deployment_scrape_success",
		"Whether the last scrape of Deployment data from the Kubernetes API succeeded (1 = success, 0 = failure).",
		nil, nil,
	)
)

// deploymentCollector reports Deployment health on every Prometheus scrape,
// reading through the same lister the JSON handler uses — so the two never
// disagree, whether that lister is direct-API or informer-backed. "unhealthy"
// isn't exposed directly; alerting rules derive it as desired != ready.
type deploymentCollector struct {
	lister DeploymentLister
}

func (c *deploymentCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- deploymentDesiredReplicasDesc
	ch <- deploymentReadyReplicasDesc
	ch <- deploymentScrapeSuccessDesc
}

func (c *deploymentCollector) Collect(ch chan<- prometheus.Metric) {
	// Bounded so a hung API server can't hang the scrape. 10s stays under
	// Prometheus's default scrape_timeout. (For the informer lister the read is
	// cache-local, so the timeout is effectively free.)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	deployments, ready, err := c.lister.ListDeployments(ctx, "")
	if err != nil || !ready {
		// An error or an unsynced cache both mean "don't trust this scrape".
		// Report it instead of silently contributing nothing — alert on == 0.
		ch <- prometheus.MustNewConstMetric(deploymentScrapeSuccessDesc, prometheus.GaugeValue, 0)
		return
	}

	ch <- prometheus.MustNewConstMetric(deploymentScrapeSuccessDesc, prometheus.GaugeValue, 1)

	for _, d := range deployments {
		desired, ready := deploymentReplicas(d.Spec.Replicas, d.Status.ReadyReplicas)
		ch <- prometheus.MustNewConstMetric(deploymentDesiredReplicasDesc, prometheus.GaugeValue, float64(desired), d.Namespace, d.Name)
		ch <- prometheus.MustNewConstMetric(deploymentReadyReplicasDesc, prometheus.GaugeValue, float64(ready), d.Namespace, d.Name)
	}
}
