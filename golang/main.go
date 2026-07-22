package main

import (
	"flag"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig, leave empty for in-cluster")
	listenAddr := flag.String("address", ":8080", "HTTP server listen address")

	flag.Parse()

	kConfig, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	kConfig.Timeout = 15 * time.Second

	clientset, err := kubernetes.NewForConfig(kConfig)
	if err != nil {
		panic(err)
	}

	// Not fatal — server still starts; /healthz reports live connectivity instead of crash-looping the pod.
	if version, err := getKubernetesVersion(clientset); err != nil {
		fmt.Printf("warning: could not reach Kubernetes API server at startup: %v\n", err)
	} else {
		fmt.Printf("Connected to Kubernetes %s\n", version)
	}

	server := NewServer(clientset)

	if err := server.Start(*listenAddr); err != nil {
		panic(err)
	}
}

// getKubernetesVersion returns the server's GitVersion, or an error if it's unreachable — doubles as a connectivity check.
func getKubernetesVersion(clientset kubernetes.Interface) (string, error) {
	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return "", err
	}

	return version.String(), nil
}
