package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// The informer lister must sync from the fake clientset and then serve the
// same data the direct lister would, so --use-informer is a drop-in.
func TestInformerLister_SyncsAndListsAll(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		deployment("default", "api", 3, 3),
		deployment("kube-system", "coredns", 1, 0),
	)

	lister := newInformerLister(clientset, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.True(t, lister.start(ctx), "cache should sync within timeout")

	got, ready, err := lister.ListDeployments(ctx, "")
	require.NoError(t, err)
	assert.True(t, ready)
	assert.Len(t, got, 2)
}

// Namespace filtering must match the direct lister's ?namespace= behaviour.
func TestInformerLister_NamespaceFilter(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		deployment("default", "api", 3, 3),
		deployment("kube-system", "coredns", 1, 0),
	)

	lister := newInformerLister(clientset, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.True(t, lister.start(ctx))

	got, ready, err := lister.ListDeployments(ctx, "default")
	require.NoError(t, err)
	assert.True(t, ready)
	require.Len(t, got, 1)
	assert.Equal(t, "api", got[0].Name)
}

// Before start() completes, the lister must report not-ready rather than an
// empty list — the whole reason the informer path is gated on HasSynced.
func TestInformerLister_NotReadyBeforeSync(t *testing.T) {
	clientset := fake.NewSimpleClientset(deployment("default", "api", 1, 1))
	lister := newInformerLister(clientset, 0)

	// Deliberately do NOT call start().
	got, ready, err := lister.ListDeployments(context.Background(), "")
	require.NoError(t, err)
	assert.False(t, ready, "cache should report not-ready before sync")
	assert.Nil(t, got)
}

// The direct lister always reports ready=true with fresh data, so the default
// path is unaffected by the ready bool.
func TestAPILister_AlwaysReady(t *testing.T) {
	clientset := fake.NewSimpleClientset(deployment("default", "api", 2, 2))
	lister := newAPILister(clientset)

	got, ready, err := lister.ListDeployments(context.Background(), "")
	require.NoError(t, err)
	assert.True(t, ready)
	assert.Len(t, got, 1)
}
