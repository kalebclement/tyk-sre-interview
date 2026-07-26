package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig, leave empty for in-cluster")
	listenAddr := flag.String("address", ":8080", "HTTP server listen address")
	useInformer := flag.Bool("use-informer", false, "read Deployments from a shared informer cache instead of direct API calls (prototype; see PR)")

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

	// SIGTERM is what kubelet sends on pod termination
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	var server *Server
	if *useInformer {
		// Prototype path: informer cache. Start it and block on the initial
		// sync before serving, so /deployments never answers from an empty
		// cache. resync 0 = rely on watch events only.
		lister := newInformerLister(clientset, 0)
		slog.Info("starting deployment informer, waiting for cache sync")
		if !lister.start(ctx) {
			slog.Error("informer cache failed to sync before shutdown")
			os.Exit(1)
		}
		slog.Info("informer cache synced")
		server = NewServerWithLister(clientset, lister)
	} else {
		server = NewServer(clientset)
	}

	if err := server.Start(ctx, *listenAddr); err != nil {
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
