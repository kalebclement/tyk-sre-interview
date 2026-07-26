package main

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listersappsv1 "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
)

// DeploymentLister is the read side of the tool. The handler and the collector
// both go through it, so they always fetch Deployments the same way. Two
// implementations: direct API list (default) and informer cache (opt-in). One
// interface means switching between them is a flag, not a rewrite.
type DeploymentLister interface {
	// ListDeployments returns Deployments in a namespace, or all namespaces if
	// empty. The bool reports whether the source is trustworthy right now: the
	// direct lister is always true; the informer is false until its cache has
	// synced, so callers don't serve an empty list as if it were real.
	ListDeployments(ctx context.Context, namespace string) ([]appsv1.Deployment, bool, error)
}

// apiLister lists straight from the API server every call. The default: no
// cache to go stale, and it errors on API failure instead of serving old data.
type apiLister struct {
	clientset kubernetes.Interface
}

func newAPILister(clientset kubernetes.Interface) *apiLister {
	return &apiLister{clientset: clientset}
}

func (l *apiLister) ListDeployments(ctx context.Context, namespace string) ([]appsv1.Deployment, bool, error) {
	list, err := l.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, true, err
	}
	return list.Items, true, nil
}

// informerLister reads from a local cache: no API round-trip per request, which
// is the win at thousands of Deployments. The catch, and why it's opt-in: if
// the API server goes away, the cache keeps serving its last state. A stale
// "all healthy" during an outage is worse than an error, so reads report
// not-ready until the cache has synced.
type informerLister struct {
	factory  informers.SharedInformerFactory
	lister   listersappsv1.DeploymentLister
	informer cache.SharedIndexInformer
	synced   cache.InformerSynced
}

func newInformerLister(clientset kubernetes.Interface, resync time.Duration) *informerLister {
	factory := informers.NewSharedInformerFactory(clientset, resync)
	di := factory.Apps().V1().Deployments()
	return &informerLister{
		factory:  factory,
		lister:   di.Lister(),
		informer: di.Informer(),
		synced:   di.Informer().HasSynced,
	}
}

// start begins the watch and blocks until the cache syncs or ctx is cancelled.
// Returns true if it synced.
func (l *informerLister) start(ctx context.Context) bool {
	l.factory.Start(ctx.Done())
	return cache.WaitForCacheSync(ctx.Done(), l.synced)
}

func (l *informerLister) ListDeployments(_ context.Context, namespace string) ([]appsv1.Deployment, bool, error) {
	if !l.synced() {
		// Not ready: report untrustworthy rather than an empty list that reads
		// as "zero deployments, all healthy".
		return nil, false, nil
	}

	var (
		ptrs []*appsv1.Deployment
		err  error
	)
	if namespace == "" {
		ptrs, err = l.lister.List(labels.Everything())
	} else {
		ptrs, err = l.lister.Deployments(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil, true, err
	}

	out := make([]appsv1.Deployment, 0, len(ptrs))
	for _, p := range ptrs {
		out = append(out, *p)
	}
	return out, true, nil
}
