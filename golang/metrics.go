package main

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

// deploymentCollector queries Deployment health live on every Prometheus scrape,
// instead of caching whatever /deployments last computed — so the metric stays
// accurate even if nobody ever calls the JSON endpoint. "unhealthy" isn't
// exposed directly; alerting rules derive it as desired != ready.
type deploymentCollector struct {
	clientset kubernetes.Interface
}

func (c *deploymentCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- deploymentDesiredReplicasDesc
	ch <- deploymentReadyReplicasDesc
	ch <- deploymentScrapeSuccessDesc
}

func (c *deploymentCollector) Collect(ch chan<- prometheus.Metric) {
	// Bounded so a hung API server can't hang the Prometheus scrape indefinitely.
	// 10s stays under Prometheus's default scrape_timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	deployments, err := c.clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		// Surface the failure instead of silently contributing nothing — alert on == 0.
		ch <- prometheus.MustNewConstMetric(deploymentScrapeSuccessDesc, prometheus.GaugeValue, 0)
		return
	}

	ch <- prometheus.MustNewConstMetric(deploymentScrapeSuccessDesc, prometheus.GaugeValue, 1)

	for _, d := range deployments.Items {
		desired, ready := deploymentReplicas(d.Spec.Replicas, d.Status.ReadyReplicas)
		ch <- prometheus.MustNewConstMetric(deploymentDesiredReplicasDesc, prometheus.GaugeValue, float64(desired), d.Namespace, d.Name)
		ch <- prometheus.MustNewConstMetric(deploymentReadyReplicasDesc, prometheus.GaugeValue, float64(ready), d.Namespace, d.Name)
	}
}
