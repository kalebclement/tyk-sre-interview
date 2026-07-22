package main

import (
	"flag"
	"log/slog"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig, leave empty for in-cluster")
	listenAddr := flag.String("address", ":8080", "HTTP server listen address")

	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	kConfig, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		slog.Error("invalid kubernetes configuration", "err", err)
		os.Exit(1)
	}

	kConfig.Timeout = 15 * time.Second

	clientset, err := kubernetes.NewForConfig(kConfig)
	if err != nil {
		slog.Error("failed to build kubernetes client", "err", err)
		os.Exit(1)
	}

	// Not fatal — server still starts; /healthz reports live connectivity instead of crash-looping the pod.
	if version, err := getKubernetesVersion(clientset); err != nil {
		slog.Warn("could not reach kubernetes api server at startup", "err", err)
	} else {
		slog.Info("connected to kubernetes", "version", version)
	}

	server := NewServer(clientset)

	if err := server.Start(*listenAddr); err != nil {
		slog.Error("http server exited", "err", err)
		os.Exit(1)
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
