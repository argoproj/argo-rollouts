package tolerantinformer

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"

	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	testutil "github.com/argoproj/argo-rollouts/test/util"
)

// benchSetup builds a populated tolerant AnalysisRun informer with n AnalysisRuns
// in the "default" namespace. Returns a namespaced lister ready to be looped on.
// Uses only the public constructor so the same source compiles on master (pre-fix)
// and on the SetTransform branch.
func benchSetupAnalysisRunLister(tb testing.TB, n int) rolloutlisters.AnalysisRunNamespaceLister {
	template := testutil.ObjectFromPath("test/e2e/functional/analysis-run-job.yaml")
	template.SetNamespace("default")
	template.SetGenerateName("")

	objs := make([]runtime.Object, 0, n)
	for i := 0; i < n; i++ {
		cp := template.DeepCopy()
		cp.SetName(fmt.Sprintf("bench-ar-%d", i))
		objs = append(objs, cp)
	}

	dynamicClient := testutil.NewFakeDynamicClient(objs...)
	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	informer := NewTolerantAnalysisRunInformer(factory)

	stopCh := make(chan struct{})
	factory.Start(stopCh)
	synced := factory.WaitForCacheSync(stopCh)
	close(stopCh)
	for gvr, ok := range synced {
		if !ok {
			tb.Fatalf("informer for %v failed to sync", gvr)
		}
	}
	return informer.Lister().AnalysisRuns("default")
}

// BenchmarkAnalysisRunListerList measures the per-call cost of the namespaced
// AnalysisRun lister List, which is the hot path called on every Prometheus
// scrape (controller/metrics/analysis.go) and every Rollout reconcile
// (rollout/analysis.go getAnalysisRunsForRollout).
//
// Compare on master vs the SetTransform branch with:
//
//	go test -bench BenchmarkAnalysisRunListerList -benchmem -benchtime=2s \
//	  -count=5 -run x ./utils/tolerantinformer/
func BenchmarkAnalysisRunListerList(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			lister := benchSetupAnalysisRunLister(b, n)
			sel := labels.Everything()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := lister.List(sel)
				if err != nil {
					b.Fatal(err)
				}
				if len(out) != n {
					b.Fatalf("got %d items, want %d", len(out), n)
				}
			}
		})
	}
}
