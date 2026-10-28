package statefulset

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// stsOption mutates a StatefulSet built by newSts, so each test can state only the fields it
// actually cares about.
type stsOption func(*appsv1.StatefulSet)

func withReplicas(replicas int32) stsOption {
	return func(sts *appsv1.StatefulSet) { sts.Spec.Replicas = &replicas }
}

func withPartition(partition int32) stsOption {
	return func(sts *appsv1.StatefulSet) {
		sts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{Partition: &partition}
	}
}

func withOrdinalStart(start int32) stsOption {
	return func(sts *appsv1.StatefulSet) {
		sts.Spec.Ordinals = &appsv1.StatefulSetOrdinals{Start: start}
	}
}

func withStatus(status appsv1.StatefulSetStatus) stsOption {
	return func(sts *appsv1.StatefulSet) { sts.Status = status }
}

func withGeneration(generation int64) stsOption {
	return func(sts *appsv1.StatefulSet) { sts.Generation = generation }
}

func newSts(opts ...stsOption) *appsv1.StatefulSet {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "sts", Namespace: "default"},
	}
	for _, opt := range opts {
		opt(sts)
	}
	return sts
}

// partitionRecorder captures the partition value carried by each Server-Side Apply patch the
// plugin issues. Assertions go through this rather than reading the object back, because the
// object only records the value the apply landed on -- the fake client prunes a zero partition, so
// a read-back cannot tell "applied 0" apart from "never patched at all", and 0 (full promotion)
// is exactly the value several of these cases care about.
type partitionRecorder struct {
	applied []int32
}

func (r *partitionRecorder) interceptor() interceptor.Funcs {
	return interceptor.Funcs{
		Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if u, ok := obj.(*unstructured.Unstructured); ok {
				// The patch is hand-built with an int32 partition, so read the raw field rather
				// than going through NestedInt64 (which only accepts int64).
				if partition, found, err := unstructured.NestedFieldNoCopy(u.Object, "spec", "updateStrategy", "rollingUpdate", "partition"); err == nil && found {
					if v, ok := partition.(int32); ok {
						r.applied = append(r.applied, v)
					}
				}
			}
			return cl.Patch(ctx, obj, patch, opts...)
		},
	}
}

// last returns the partition from the most recent apply, failing the test if none was issued.
func (r *partitionRecorder) last(t *testing.T) int32 {
	t.Helper()
	require.NotEmpty(t, r.applied, "no partition patch was issued")
	return r.applied[len(r.applied)-1]
}

// newPlugin wires a Plugin against a fake client seeded with objs, recording applied partitions.
func newPlugin(t *testing.T, objs ...client.Object) (*Plugin, client.Client, *partitionRecorder) {
	t.Helper()
	rec := &partitionRecorder{}
	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		WithInterceptorFuncs(rec.interceptor()).
		Build()
	return &Plugin{logCtx: log.WithField("test", t.Name()), client: c}, c, rec
}

var ref = v1alpha1.WorkloadRef{Name: "sts"}

// testRP is the RolloutPlugin passed to plugin methods; the plugin reads namespace and
// workloadRef from it.
var testRP = rolloutPluginFor("default", ref)

func rolloutPluginFor(namespace string, workloadRef v1alpha1.WorkloadRef) *v1alpha1.RolloutPlugin {
	return &v1alpha1.RolloutPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: namespace},
		Spec:       v1alpha1.RolloutPluginSpec{WorkloadRef: workloadRef},
	}
}

func TestSetWeight(t *testing.T) {
	tests := []struct {
		name              string
		opts              []stsOption
		weight            int32
		expectedPartition int32
		expectNoPatch     bool
	}{
		{
			name:              "50% of 4 replicas blocks the top half",
			opts:              []stsOption{withReplicas(4)},
			weight:            50,
			expectedPartition: 2,
		},
		{
			name: "small weight still rolls one pod",
			// 3 replicas at 10% rounds up to 1 updated pod, so partition lands at 2 rather
			// than 3 (which would roll nothing while reporting the step done).
			opts:              []stsOption{withReplicas(3)},
			weight:            10,
			expectedPartition: 2,
		},
		{
			name:              "100% unblocks everything",
			opts:              []stsOption{withReplicas(3), withPartition(2)},
			weight:            100,
			expectedPartition: 0,
		},
		{
			name: "100% on an untouched StatefulSet needs no patch",
			// An unset partition already reads as 0, so the desired value is already in effect.
			opts:          []stsOption{withReplicas(3)},
			weight:        100,
			expectNoPatch: true,
		},
		{
			name: "partition already at the desired value needs no patch",
			// A redundant apply on every reconcile would bump resourceVersion and re-trigger the watch.
			opts:          []stsOption{withReplicas(4), withPartition(2)},
			weight:        50,
			expectNoPatch: true,
		},
		{
			name: "ordinal start is not added to the partition",
			// The StatefulSet controller compares the partition with the pod index (ordinal minus
			// start), so offsetting it by start would block every pod forever.
			opts:              []stsOption{withReplicas(4), withOrdinalStart(10)},
			weight:            50,
			expectedPartition: 2,
		},
		{
			name:              "nil replicas defaults to 1",
			opts:              []stsOption{withPartition(1)},
			weight:            100,
			expectedPartition: 0,
		},
		{
			name: "weight 0 blocks the partition",
			// Blocked rather than replicas, so a scale-up can't create pods on the update revision.
			opts:              []stsOption{withReplicas(3)},
			weight:            0,
			expectedPartition: blockedPartition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, rec := newPlugin(t, newSts(tt.opts...))
			require.NoError(t, p.SetWeight(context.Background(), testRP, tt.weight))
			if tt.expectNoPatch {
				assert.Empty(t, rec.applied, "no patch should be issued when the partition is already correct")
				return
			}
			assert.Equal(t, tt.expectedPartition, rec.last(t))
		})
	}
}

// podsOnRevision returns ready pods sts-<from>..sts-<to-1> on the given revision.
func podsOnRevision(from, to int32, revision string) []client.Object {
	var pods []client.Object
	for i := from; i < to; i++ {
		pods = append(pods, readyPod(fmt.Sprintf("sts-%d", i), "default", revision))
	}
	return pods
}

func notReady(pod client.Object) client.Object {
	p := pod.(*corev1.Pod)
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
	return p
}

func readySince(pod client.Object, since time.Time) client.Object {
	p := pod.(*corev1.Pod)
	p.Status.Conditions[0].LastTransitionTime = metav1.NewTime(since)
	return p
}

func terminating(pod client.Object) client.Object {
	p := pod.(*corev1.Pod)
	now := metav1.Now()
	p.DeletionTimestamp = &now
	p.Finalizers = []string{"test/keep"}
	return p
}

func TestVerifyWeight(t *testing.T) {
	rolled := appsv1.StatefulSetStatus{UpdateRevision: "new", CurrentRevision: "stable"}
	tests := []struct {
		name     string
		opts     []stsOption
		pods     []client.Object
		weight   int32
		verified bool
	}{
		{
			name: "partition not yet applied",
			// SetWeight's patch hasn't landed in the cache yet; reporting verified here would
			// advance the step before any pod rolled.
			opts:     []stsOption{withReplicas(4), withPartition(4), withStatus(rolled)},
			weight:   50,
			verified: false,
		},
		{
			name: "status not yet observed latest generation",
			opts: []stsOption{withReplicas(4), withPartition(2), withGeneration(3),
				withStatus(appsv1.StatefulSetStatus{UpdateRevision: "new", ObservedGeneration: 2, UpdatedReplicas: 2})},
			pods:     podsOnRevision(2, 4, "new"),
			weight:   50,
			verified: false,
		},
		{
			name:     "partition applied but pods have not rolled",
			opts:     []stsOption{withReplicas(4), withPartition(2), withStatus(rolled)},
			pods:     append(podsOnRevision(0, 3, "stable"), podsOnRevision(3, 4, "new")...),
			weight:   50,
			verified: false,
		},
		{
			name:     "partition applied and canary pods available",
			opts:     []stsOption{withReplicas(4), withPartition(2), withStatus(rolled)},
			pods:     append(podsOnRevision(0, 2, "stable"), podsOnRevision(2, 4, "new")...),
			weight:   50,
			verified: true,
		},
		{
			name: "canary pod created but not ready is not counted",
			// status.updatedReplicas counts sts-3 even while it crash-loops.
			opts: []stsOption{withReplicas(4), withPartition(3),
				withStatus(appsv1.StatefulSetStatus{UpdateRevision: "new", UpdatedReplicas: 1, ReadyReplicas: 3})},
			pods:     append(podsOnRevision(0, 3, "stable"), notReady(readyPod("sts-3", "default", "new"))),
			weight:   25,
			verified: false,
		},
		{
			name: "canary pod ready for less than minReadySeconds is not counted",
			opts: []stsOption{withReplicas(4), withPartition(3), withStatus(rolled),
				func(sts *appsv1.StatefulSet) { sts.Spec.MinReadySeconds = 30 }},
			pods:     append(podsOnRevision(0, 3, "stable"), readySince(readyPod("sts-3", "default", "new"), time.Now().Add(-5*time.Second))),
			weight:   25,
			verified: false,
		},
		{
			name: "canary pod ready for at least minReadySeconds is counted",
			opts: []stsOption{withReplicas(4), withPartition(3), withStatus(rolled),
				func(sts *appsv1.StatefulSet) { sts.Spec.MinReadySeconds = 30 }},
			pods:     append(podsOnRevision(0, 3, "stable"), readySince(readyPod("sts-3", "default", "new"), time.Now().Add(-time.Minute))),
			weight:   25,
			verified: true,
		},
		{
			name:     "terminating canary pod is not counted",
			opts:     []stsOption{withReplicas(4), withPartition(3), withStatus(rolled)},
			pods:     append(podsOnRevision(0, 3, "stable"), terminating(readyPod("sts-3", "default", "new"))),
			weight:   25,
			verified: false,
		},
		{
			name: "pod below the partition still on the update revision is not verified",
			// Left over from a higher weight or created by a scale-up; more pods than the weight
			// allows run the update until SetWeight rolls it back.
			opts:     []stsOption{withReplicas(4), withPartition(2), withStatus(rolled)},
			pods:     append(podsOnRevision(0, 1, "stable"), podsOnRevision(1, 4, "new")...),
			weight:   50,
			verified: false,
		},
		{
			name:     "ordinal start uses the same math as SetWeight",
			opts:     []stsOption{withReplicas(4), withOrdinalStart(10), withPartition(2), withStatus(rolled)},
			pods:     append(podsOnRevision(10, 12, "stable"), podsOnRevision(12, 14, "new")...),
			weight:   50,
			verified: true,
		},
		{
			name:     "weight 0 verifies once the partition is blocked and pods are stable",
			opts:     []stsOption{withReplicas(3), withPartition(blockedPartition), withStatus(rolled)},
			pods:     podsOnRevision(0, 3, "stable"),
			weight:   0,
			verified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := newPlugin(t, append([]client.Object{newSts(tt.opts...)}, tt.pods...)...)
			verified, err := p.VerifyWeight(context.Background(), testRP, tt.weight)
			require.NoError(t, err)
			assert.Equal(t, tt.verified, verified)
		})
	}
}

func TestPromoteFull_SetsPartitionToZero(t *testing.T) {
	p, _, rec := newPlugin(t, newSts(withReplicas(4), withPartition(3)))
	require.NoError(t, p.PromoteFull(context.Background(), testRP))
	assert.Equal(t, int32(0), rec.last(t), "promotion must unblock every ordinal")
}

func TestGetResourceStatus(t *testing.T) {
	tests := []struct {
		name     string
		opts     []stsOption
		expected func() *appsv1.StatefulSetStatus // unused marker; see assertions below
		assert   func(t *testing.T, replicas, updated, ready int32, isReady bool)
	}{
		{
			name: "converged StatefulSet reports ready",
			opts: []stsOption{withReplicas(3), withGeneration(2), withStatus(appsv1.StatefulSetStatus{
				ObservedGeneration: 2, ReadyReplicas: 3, UpdatedReplicas: 3, AvailableReplicas: 3,
			})},
			assert: func(t *testing.T, replicas, updated, ready int32, isReady bool) {
				assert.Equal(t, int32(3), replicas)
				assert.Equal(t, int32(3), updated)
				assert.Equal(t, int32(3), ready)
				assert.True(t, isReady)
			},
		},
		{
			name: "stale observedGeneration is not ready",
			// ReadyReplicas still reflects the pre-change spec, so treating this as ready
			// would let a step advance before the StatefulSet controller even reacted.
			opts: []stsOption{withReplicas(3), withGeneration(3), withStatus(appsv1.StatefulSetStatus{
				ObservedGeneration: 2, ReadyReplicas: 3,
			})},
			assert: func(t *testing.T, replicas, updated, ready int32, isReady bool) {
				assert.False(t, isReady)
			},
		},
		{
			name: "fewer ready replicas than desired is not ready",
			opts: []stsOption{withReplicas(3), withGeneration(1), withStatus(appsv1.StatefulSetStatus{
				ObservedGeneration: 1, ReadyReplicas: 2,
			})},
			assert: func(t *testing.T, replicas, updated, ready int32, isReady bool) {
				assert.False(t, isReady)
			},
		},
		{
			name: "updatedReplicas is clamped to the replica count",
			// A scale-down can leave status.updatedReplicas above spec.replicas; an unclamped
			// value would report more updated pods than exist.
			opts: []stsOption{withReplicas(2), withStatus(appsv1.StatefulSetStatus{UpdatedReplicas: 5})},
			assert: func(t *testing.T, replicas, updated, ready int32, isReady bool) {
				assert.Equal(t, int32(2), updated)
			},
		},
		{
			name: "nil replicas defaults to 1",
			opts: []stsOption{withStatus(appsv1.StatefulSetStatus{ReadyReplicas: 1})},
			assert: func(t *testing.T, replicas, updated, ready int32, isReady bool) {
				assert.Equal(t, int32(1), replicas)
				assert.True(t, isReady)
			},
		},
		{
			name: "a pod left on an earlier canary revision is not ready",
			// Pods 3-4 still run the previous canary (neither current nor update revision), so
			// an abort that only checked for zero updated pods would stop before rolling them back.
			opts: []stsOption{withReplicas(5), withGeneration(1), withStatus(appsv1.StatefulSetStatus{
				ObservedGeneration: 1, ReadyReplicas: 5, CurrentRevision: "a", UpdateRevision: "c", CurrentReplicas: 3,
			})},
			assert: func(t *testing.T, replicas, updated, ready int32, isReady bool) {
				assert.Equal(t, int32(0), updated)
				assert.False(t, isReady)
			},
		},
		{
			name: "every pod on the current or update revision is ready",
			opts: []stsOption{withReplicas(5), withGeneration(1), withStatus(appsv1.StatefulSetStatus{
				ObservedGeneration: 1, ReadyReplicas: 5, CurrentRevision: "a", UpdateRevision: "c", CurrentReplicas: 4, UpdatedReplicas: 1,
			})},
			assert: func(t *testing.T, replicas, updated, ready int32, isReady bool) {
				assert.True(t, isReady)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := newPlugin(t, newSts(tt.opts...))
			status, err := p.GetResourceStatus(context.Background(), testRP)
			require.NoError(t, err)
			tt.assert(t, status.Replicas, status.UpdatedReplicas, status.ReadyReplicas, status.Ready)
		})
	}
}

// TestGetResourceStatus_NotReadyUntilCacheObservesOwnPatch: a cached StatefulSet from before the
// plugin's own partition patch still looks fully observed and ready, so reporting it Ready would
// complete an abort before anything rolled back. The status reports the resourceVersion it was
// computed from, so the controller can tell how far behind the cache is.
func TestGetResourceStatus_NotReadyUntilCacheObservesOwnPatch(t *testing.T) {
	sts := newSts(withReplicas(1), withGeneration(1), withStatus(appsv1.StatefulSetStatus{
		ObservedGeneration: 1, ReadyReplicas: 1, CurrentRevision: "stable", UpdateRevision: "new", CurrentReplicas: 1,
	}))
	sts.UID = "sts-uid"
	cachedRV := "1"
	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(sts).
		WithInterceptorFuncs(interceptor.Funcs{
			// The cache serves the StatefulSet at cachedRV, lagging the API server.
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if err := cl.Get(ctx, key, obj, opts...); err != nil {
					return err
				}
				if _, ok := obj.(*appsv1.StatefulSet); ok {
					obj.SetResourceVersion(cachedRV)
				}
				return nil
			},
			// The API server accepts the patch at a resourceVersion newer than the cache's.
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if err := cl.Patch(ctx, obj, patch, opts...); err != nil {
					return err
				}
				obj.SetResourceVersion("2")
				return nil
			},
		}).Build()
	p := &Plugin{logCtx: log.WithField("test", t.Name()), client: c}

	require.NoError(t, p.Abort(context.Background(), testRP))
	status, err := p.GetResourceStatus(context.Background(), testRP)
	require.NoError(t, err)
	assert.False(t, status.Ready, "a cached StatefulSet older than the plugin's own patch must not be Ready")
	assert.Equal(t, "1", status.ObservedResourceVersion)

	cachedRV = "2"
	status, err = p.GetResourceStatus(context.Background(), testRP)
	require.NoError(t, err)
	assert.True(t, status.Ready)
	assert.Equal(t, "2", status.ObservedResourceVersion)
}

func TestRestart_BlocksPartition(t *testing.T) {
	p, _, rec := newPlugin(t, newSts(withReplicas(3), withPartition(1)))
	require.NoError(t, p.Restart(context.Background(), testRP))
	assert.Equal(t, blockedPartition, rec.last(t),
		"restart must block the partition so recreated pods land on CurrentRevision")
}

// TestDeletionCleanup verifies the plugin drops only its own Server-Side Apply ownership of the
// partition on deletion: other owners' entries and the partition value are left as they are, and
// a workload it doesn't manage or that is gone is not an error.
func TestDeletionCleanup(t *testing.T) {
	stsRef := v1alpha1.WorkloadRef{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "sts"}
	entry := func(manager string, op metav1.ManagedFieldsOperationType) metav1.ManagedFieldsEntry {
		return metav1.ManagedFieldsEntry{
			Manager: manager, Operation: op, APIVersion: "apps/v1", FieldsType: "FieldsV1",
			FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:updateStrategy":{"f:rollingUpdate":{"f:partition":{}}}}}`)},
		}
	}
	argocd := entry("argocd-controller", metav1.ManagedFieldsOperationApply)
	plugin := entry(FieldManager, metav1.ManagedFieldsOperationApply)

	// patchMetadata is the metadata a cleanup patch writes; the patch must not touch the spec.
	type patchMetadata struct {
		Metadata struct {
			ManagedFields   []metav1.ManagedFieldsEntry `json:"managedFields"`
			ResourceVersion string                      `json:"resourceVersion"`
		} `json:"metadata"`
		Spec map[string]any `json:"spec"`
	}

	tests := []struct {
		name        string
		workloadRef v1alpha1.WorkloadRef
		sts         *appsv1.StatefulSet
		getErr      error
		patchErr    error
		// wantManagedFields is the managedFields the patch sets; nil means no patch is expected.
		wantManagedFields []metav1.ManagedFieldsEntry
		wantErr           string
	}{
		{
			name:              "drops only the plugin's entry",
			workloadRef:       stsRef,
			sts:               newSts(withPartition(2)),
			wantManagedFields: []metav1.ManagedFieldsEntry{argocd},
		},
		{
			name:        "sole entry is cleared with a single empty entry",
			workloadRef: stsRef,
			sts: func() *appsv1.StatefulSet {
				sts := newSts(withPartition(2))
				sts.ManagedFields = []metav1.ManagedFieldsEntry{plugin}
				return sts
			}(),
			wantManagedFields: []metav1.ManagedFieldsEntry{{}},
		},
		{
			name:        "no entry of the plugin is a no-op",
			workloadRef: stsRef,
			sts: func() *appsv1.StatefulSet {
				sts := newSts(withPartition(2))
				sts.ManagedFields = []metav1.ManagedFieldsEntry{argocd}
				return sts
			}(),
		},
		{
			name:        "workloadRef the plugin doesn't manage is a no-op",
			workloadRef: v1alpha1.WorkloadRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "sts"},
			sts:         newSts(withPartition(2)),
		},
		{
			name:        "missing workload is a no-op",
			workloadRef: stsRef,
		},
		{
			name:        "read error is returned",
			workloadRef: stsRef,
			sts:         newSts(withPartition(2)),
			getErr:      fmt.Errorf("connection refused"),
			wantErr:     "failed to get StatefulSet: connection refused",
		},
		{
			name:              "patch error is returned",
			workloadRef:       stsRef,
			sts:               newSts(withPartition(2)),
			patchErr:          apierrors.NewConflict(appsv1.Resource("statefulsets"), "sts", fmt.Errorf("modified")),
			wantManagedFields: []metav1.ManagedFieldsEntry{argocd},
			wantErr:           "failed to drop partition ownership",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := []client.Object{}
			if tt.sts != nil {
				if tt.sts.ManagedFields == nil {
					tt.sts.ManagedFields = []metav1.ManagedFieldsEntry{argocd, plugin}
				}
				objs = append(objs, tt.sts)
			}
			var patches []patchMetadata
			c := fake.NewClientBuilder().
				WithScheme(testScheme(t)).
				WithObjects(objs...).
				WithReturnManagedFields().
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if tt.getErr != nil {
							return tt.getErr
						}
						return cl.Get(ctx, key, obj, opts...)
					},
					Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						assert.Equal(t, types.MergePatchType, patch.Type())
						data, err := patch.Data(obj)
						require.NoError(t, err)
						var p patchMetadata
						require.NoError(t, json.Unmarshal(data, &p))
						patches = append(patches, p)
						if tt.patchErr != nil {
							return tt.patchErr
						}
						return cl.Patch(ctx, obj, patch, opts...)
					},
				}).
				Build()
			p := &Plugin{logCtx: log.WithField("test", t.Name()), client: c, apiReader: c}

			err := p.DeletionCleanup(context.Background(), rolloutPluginFor("default", tt.workloadRef))
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			if tt.wantManagedFields == nil {
				assert.Empty(t, patches, "no patch expected")
				return
			}
			require.Len(t, patches, 1)
			assert.Equal(t, tt.wantManagedFields, patches[0].Metadata.ManagedFields)
			assert.NotEmpty(t, patches[0].Metadata.ResourceVersion, "the patch must be guarded by an optimistic lock")
			assert.Nil(t, patches[0].Spec, "the partition value must be left as it is")
			if tt.patchErr != nil {
				return
			}

			var got appsv1.StatefulSet
			require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "sts"}, &got))
			assert.Equal(t, int32(2), *got.Spec.UpdateStrategy.RollingUpdate.Partition)
			for _, e := range got.ManagedFields {
				assert.NotEqual(t, FieldManager, e.Manager, "the plugin must no longer own any field")
			}
		})
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return scheme
}

func readyPod(name, namespace, revision string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{appsv1.StatefulSetRevisionLabel: revision},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

func TestAbort_PatchesPartitionBeforeDeletingPods(t *testing.T) {
	scheme := testScheme(t)
	replicas := int32(2)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "sts", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
		Status:     appsv1.StatefulSetStatus{CurrentRevision: "stable"},
	}
	pods := []client.Object{
		readyPod("sts-0", "default", "stable"),
		readyPod("sts-1", "default", "new"), // still on new revision, needs rollback
	}
	objs := append([]client.Object{sts}, pods...)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	p := &Plugin{logCtx: log.WithField("test", "abort"), client: c}

	rp := rolloutPluginFor("default", v1alpha1.WorkloadRef{Name: "sts"})

	// First call: partition isn't blocking yet, so it patches without touching any pod.
	require.NoError(t, p.Abort(context.Background(), rp))

	var patched appsv1.StatefulSet
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "sts", Namespace: "default"}, &patched))
	require.NotNil(t, patched.Spec.UpdateStrategy.RollingUpdate)
	require.NotNil(t, patched.Spec.UpdateStrategy.RollingUpdate.Partition)
	assert.Equal(t, blockedPartition, *patched.Spec.UpdateStrategy.RollingUpdate.Partition)

	// Second call: partition now blocking, sts-1 is still on "new" — must be deleted, one pod
	// per call.
	require.NoError(t, p.Abort(context.Background(), rp))

	var pod1 corev1.Pod
	err := c.Get(context.Background(), client.ObjectKey{Name: "sts-1", Namespace: "default"}, &pod1)
	assert.True(t, apierrors.IsNotFound(err), "sts-1 (still on the new revision) must be deleted by the second Abort call")

	var pod0 corev1.Pod
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "sts-0", Namespace: "default"}, &pod0))
	assert.Nil(t, pod0.DeletionTimestamp, "sts-0 is already on the stable revision and must not be touched")
}

// Args come from the plugin's resourcePlugins ConfigMap entry. Each flag overrides only its own
// setting; anything not set falls back to the controller's logger, which must stay untouched.
func TestNewPlugin(t *testing.T) {
	newBase := func() *log.Entry {
		l := log.New()
		l.SetLevel(log.InfoLevel)
		l.SetFormatter(&log.TextFormatter{})
		return l.WithField("plugin", "argoproj/statefulset")
	}

	t.Run("no args reuses the controller logger", func(t *testing.T) {
		base := newBase()
		cfg := &rest.Config{}
		p, err := NewPlugin(base, cfg, nil)
		require.NoError(t, err)
		assert.Same(t, base, p.logCtx)
		assert.Same(t, cfg, p.restConfig)
	})

	tests := []struct {
		name          string
		args          []string
		wantLevel     log.Level
		wantJSON      bool
		keepFormatter bool
	}{
		{name: "loglevel only keeps controller format", args: []string{"--loglevel", "debug"}, wantLevel: log.DebugLevel, keepFormatter: true},
		{name: "logformat only keeps controller level", args: []string{"--logformat", "JSON"}, wantLevel: log.InfoLevel, wantJSON: true},
		{name: "both flags, single-dash and = syntax", args: []string{"-loglevel=warn", "-logformat=text"}, wantLevel: log.WarnLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := newBase()
			baseFormatter := base.Logger.Formatter
			cfg := &rest.Config{}
			p, err := NewPlugin(base, cfg, tt.args)
			require.NoError(t, err)
			assert.Same(t, cfg, p.restConfig)

			assert.Equal(t, tt.wantLevel, p.logCtx.Logger.GetLevel())
			_, isJSON := p.logCtx.Logger.Formatter.(*log.JSONFormatter)
			assert.Equal(t, tt.wantJSON, isJSON)
			if tt.keepFormatter {
				assert.Same(t, baseFormatter, p.logCtx.Logger.Formatter)
			}
			assert.Equal(t, "argoproj/statefulset", p.logCtx.Data["plugin"], "entry fields must be preserved")

			assert.NotSame(t, base.Logger, p.logCtx.Logger)
			assert.Equal(t, log.InfoLevel, base.Logger.GetLevel(), "controller log level must not change")
			assert.Same(t, baseFormatter, base.Logger.Formatter, "controller log format must not change")
		})
	}
}

func TestNewPlugin_RejectsInvalidArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "unknown flag", args: []string{"--kubeClientQPS", "40"}, wantErr: "flag provided but not defined"},
		{name: "invalid loglevel", args: []string{"--loglevel", "verbose"}, wantErr: "not a valid logrus Level"},
		{name: "invalid logformat", args: []string{"--logformat", "xml"}, wantErr: `unknown logformat "xml"`},
		{name: "flag missing value", args: []string{"--loglevel"}, wantErr: "flag needs an argument"},
		{name: "positional argument", args: []string{"debug"}, wantErr: "unexpected positional arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPlugin(log.NewEntry(log.New()), &rest.Config{}, tt.args)
			require.Error(t, err)
			assert.Nil(t, p)
			assert.Contains(t, err.Error(), "invalid args for statefulset plugin")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		ref     v1alpha1.WorkloadRef
		config  string
		wantErr string
	}{
		{name: "statefulset without config", ref: v1alpha1.WorkloadRef{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "web"}},
		{name: "empty config object", ref: v1alpha1.WorkloadRef{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "web"}, config: `{}`},
		{name: "null config", ref: v1alpha1.WorkloadRef{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "web"}, config: `null`},
		{name: "wrong kind", ref: v1alpha1.WorkloadRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "web"}, wantErr: "spec.workloadRef must reference a apps/v1 StatefulSet, got apps/v1 Deployment"},
		{name: "wrong apiVersion", ref: v1alpha1.WorkloadRef{APIVersion: "apps/v1beta2", Kind: "StatefulSet", Name: "web"}, wantErr: "got apps/v1beta2 StatefulSet"},
		{name: "unsupported config", ref: v1alpha1.WorkloadRef{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "web"}, config: `{"maxSurge":2}`, wantErr: `spec.plugin.config is not supported by the statefulset plugin currently, got {"maxSurge":2}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := &v1alpha1.RolloutPlugin{Spec: v1alpha1.RolloutPluginSpec{
				WorkloadRef: tt.ref,
				Plugin:      v1alpha1.PluginStep{Name: "argoproj/statefulset", Config: []byte(tt.config)},
			}}
			err := (&Plugin{logCtx: log.NewEntry(log.New())}).Validate(context.Background(), rp)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
