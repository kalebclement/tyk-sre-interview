package main

import (
	"context"

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
}

func (c *deploymentCollector) Collect(ch chan<- prometheus.Metric) {
	deployments, err := c.clientset.AppsV1().Deployments("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return // a scrape error here just means this collector contributes nothing this round
	}

	for _, d := range deployments.Items {
		desired, ready := deploymentReplicas(d.Spec.Replicas, d.Status.ReadyReplicas)
		ch <- prometheus.MustNewConstMetric(deploymentDesiredReplicasDesc, prometheus.GaugeValue, float64(desired), d.Namespace, d.Name)
		ch <- prometheus.MustNewConstMetric(deploymentReadyReplicasDesc, prometheus.GaugeValue, float64(ready), d.Namespace, d.Name)
	}
}
