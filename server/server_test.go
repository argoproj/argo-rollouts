package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func TestListAllPodsCached(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Mock Kubernetes client
	kubeClient := fake.NewSimpleClientset()

	// Create a test server
	server := &ArgoRolloutsServer{
		Options: ServerOptions{
			KubeClientset: kubeClient,
			CacheTTL:      5 * time.Minute, // Default TTL
		},
		PodCache: sync.Map{},
	}

	namespace := "test-namespace"

	// Test Case 1: Cache Disabled
	t.Run("Cache Disabled", func(t *testing.T) {
		server.Options.CacheTTL = 0 // Disable cache

		// Mock API response
		podList := &corev1.PodList{
			Items: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
			},
		}
		kubeClient.Fake.PrependReactor("list", "pods", func(action clientgotesting.Action) (bool, runtime.Object, error) {
			return true, podList, nil
		})

		pods, err := listAllPodsCached(context.TODO(), namespace, server)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pods) != 2 {
			t.Fatalf("expected 2 pods, got %d", len(pods))
		}
	})

	// Test Case 2: Cache Hit
	t.Run("Cache Hit", func(t *testing.T) {
		server.Options.CacheTTL = 5 * time.Minute // Enable cache

		// Add data to the cache
		cachedPods := []*corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "cached-pod-1"}},
		}
		server.PodCache.Store(namespace, PodCacheItem{
			Data:      cachedPods,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		})

		pods, err := listAllPodsCached(context.TODO(), namespace, server)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pods) != 1 || pods[0].Name != "cached-pod-1" {
			t.Fatalf("expected cached pod, got %v", pods)
		}
	})

	// Test Case 3: Cache Miss
	t.Run("Cache Miss", func(t *testing.T) {
		server.Options.CacheTTL = 5 * time.Minute // Enable cache

		// Mock API response
		podList := &corev1.PodList{
			Items: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "pod-3"}},
			},
		}
		kubeClient.Fake.PrependReactor("list", "pods", func(action clientgotesting.Action) (bool, runtime.Object, error) {
			return true, podList, nil
		})

		// Ensure cache is empty
		server.PodCache.Delete(namespace)

		pods, err := listAllPodsCached(context.TODO(), namespace, server)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pods) != 1 || pods[0].Name != "pod-3" {
			t.Fatalf("expected pod from API, got %v", pods)
		}
	})
}

func TestListAllReplicaSetsCached(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Mock Kubernetes client
	kubeClient := fake.NewSimpleClientset()

	// Create a test server
	server := &ArgoRolloutsServer{
		Options: ServerOptions{
			KubeClientset: kubeClient,
			CacheTTL:      5 * time.Minute, // Default TTL
		},
		ReplicaSetCache: sync.Map{},
	}

	namespace := "test-namespace"

	// Test Case 1: Cache Disabled
	t.Run("Cache Disabled", func(t *testing.T) {
		server.Options.CacheTTL = 0 // Disable cache

		// Mock API response
		rsList := &appsv1.ReplicaSetList{
			Items: []appsv1.ReplicaSet{
				{ObjectMeta: metav1.ObjectMeta{Name: "rs-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "rs-2"}},
			},
		}
		kubeClient.Fake.PrependReactor("list", "replicasets", func(action clientgotesting.Action) (bool, runtime.Object, error) {
			return true, rsList, nil
		})

		replicaSets, err := listAllReplicaSetsCached(context.TODO(), namespace, server)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(replicaSets) != 2 {
			t.Fatalf("expected 2 replica sets, got %d", len(replicaSets))
		}
	})

	// Test Case 2: Cache Hit
	t.Run("Cache Hit", func(t *testing.T) {
		server.Options.CacheTTL = 5 * time.Minute // Enable cache

		// Add data to the cache
		cachedRS := []*appsv1.ReplicaSet{
			{ObjectMeta: metav1.ObjectMeta{Name: "cached-rs-1"}},
		}
		server.ReplicaSetCache.Store(namespace, ReplicaSetCacheItem{
			Data:      cachedRS,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		})

		replicaSets, err := listAllReplicaSetsCached(context.TODO(), namespace, server)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(replicaSets) != 1 || replicaSets[0].Name != "cached-rs-1" {
			t.Fatalf("expected cached replica set, got %v", replicaSets)
		}
	})

	// Test Case 3: Cache Miss
	t.Run("Cache Miss", func(t *testing.T) {
		server.Options.CacheTTL = 5 * time.Minute // Enable cache

		// Mock API response
		rsList := &appsv1.ReplicaSetList{
			Items: []appsv1.ReplicaSet{
				{ObjectMeta: metav1.ObjectMeta{Name: "rs-3"}},
			},
		}
		kubeClient.Fake.PrependReactor("list", "replicasets", func(action clientgotesting.Action) (bool, runtime.Object, error) {
			return true, rsList, nil
		})

		// Ensure cache is empty
		server.ReplicaSetCache.Delete(namespace)

		replicaSets, err := listAllReplicaSetsCached(context.TODO(), namespace, server)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(replicaSets) != 1 || replicaSets[0].Name != "rs-3" {
			t.Fatalf("expected replica set from API, got %v", replicaSets)
		}
	})
}
