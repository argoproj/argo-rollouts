package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/analysis"
	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/utils/record"
	traceutil "github.com/argoproj/argo-rollouts/utils/trace"
)

func TestControllerTraceCanaryGateFlow(t *testing.T) {
	for _, phase := range []v1alpha1.AnalysisPhase{v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseFailed} {
		t.Run(string(phase), func(t *testing.T) {
			flow := newControllerTraceFlow(t, phase)
			rollout, run, creator := flow.createGate(t)
			terminal, result := flow.analyzeGate(t, rollout, run)
			flow.consumeGate(t, rollout, terminal, creator, result)
		})
	}
}

type controllerTraceFlow struct {
	fixture    *fixture
	controller *Controller
	argo       informers.SharedInformerFactory
	kube       kubeinformers.SharedInformerFactory
	recorder   *tracetest.SpanRecorder
	phase      v1alpha1.AnalysisPhase
}

func newControllerTraceFlow(t *testing.T, phase v1alpha1.AnalysisPhase) *controllerTraceFlow {
	t.Helper()
	f := newFixture(t)
	t.Cleanup(f.Close)
	// The handoff clock is real time, so attempt timestamps must advance too.
	require.NoError(t, f.unfreezeTime())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		value := "0.25"
		if phase == v1alpha1.AnalysisPhaseSuccessful {
			value = "0"
		}
		_, _ = fmt.Fprintf(w, `{"errorRate":%s}`, value)
	}))
	t.Cleanup(server.Close)
	template := analysisTemplate("gate")
	template.Spec = v1alpha1.AnalysisTemplateSpec{Metrics: []v1alpha1.Metric{{
		Name: "error-rate", Count: ptr.To(intstr.FromInt(1)), SuccessCondition: "result < 0.05",
		Provider: v1alpha1.MetricProvider{Web: &v1alpha1.WebMetric{URL: server.URL, JSONPath: "{$.errorRate}"}},
	}}}
	steps := []v1alpha1.CanaryStep{{Analysis: &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: template.Name}},
	}}}
	stable := newCanaryRollout("checkout", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	candidate := bumpVersion(stable)
	stableRS := newReplicaSetWithStatus(stable, 1, 1)
	candidateRS := newReplicaSetWithStatus(candidate, 0, 0)
	candidate = updateCanaryRolloutStatus(candidate, stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], 1, 0, 1, false)
	candidate.ResourceVersion = "10"
	f.kubeobjects = []runtime.Object{stableRS, candidateRS}
	f.replicaSetLister = []*appsv1.ReplicaSet{stableRS, candidateRS}
	f.objects = []runtime.Object{candidate, template}
	f.rolloutLister = []*v1alpha1.Rollout{candidate}
	f.analysisTemplateLister = []*v1alpha1.AnalysisTemplate{template}
	controller, argo, kube := f.newController(noResyncPeriodFunc)
	t.Cleanup(func() {
		controller.rolloutWorkqueue.ShutDown()
		controller.serviceWorkqueue.ShutDown()
		controller.ingressWorkqueue.ShutDown()
	})

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
		otel.SetTracerProvider(previousProvider)
	})
	controller.tracer = provider.Tracer("controller-flow-test")
	controller.traceHandoffs = traceutil.NewHandoffs()
	// Fake Kubernetes clients do not assign API identities or resource versions.
	f.client.PrependReactor("create", "analysisruns", func(action clienttesting.Action) (bool, runtime.Object, error) {
		run := action.(clienttesting.CreateAction).GetObject().(*v1alpha1.AnalysisRun)
		run.UID, run.Generation, run.ResourceVersion = types.UID("gate-uid"), 1, "1"
		return false, nil, nil
	})
	f.client.PrependReactor("patch", "*", controllerTraceVersionedPatch(f))
	return &controllerTraceFlow{fixture: f, controller: controller, argo: argo, kube: kube, recorder: recorder, phase: phase}
}

func (f *controllerTraceFlow) createGate(t *testing.T) (*v1alpha1.Rollout, *v1alpha1.AnalysisRun, sdktrace.ReadOnlySpan) {
	t.Helper()
	require.NoError(t, f.controller.syncHandler(context.Background(), "default/checkout"))
	rollout, err := f.fixture.client.ArgoprojV1alpha1().Rollouts("default").Get(context.Background(), "checkout", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, rollout.Status.Canary.CurrentStepAnalysisRunStatus)
	run, err := f.fixture.client.ArgoprojV1alpha1().AnalysisRuns("default").Get(context.Background(), rollout.Status.Canary.CurrentStepAnalysisRunStatus.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, f.argo.Argoproj().V1alpha1().Rollouts().Informer().GetIndexer().Update(rollout))
	require.NoError(t, f.argo.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Add(run))
	spans := f.recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "controller.Rollout", spans[0].Name())
	assert.False(t, spans[0].Parent().IsValid())
	return rollout, run, spans[0]
}

func (f *controllerTraceFlow) analyzeGate(t *testing.T, rollout *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) (*v1alpha1.AnalysisRun, sdktrace.ReadOnlySpan) {
	t.Helper()
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "trace-flow-analysis")
	controller := analysis.NewController(analysis.ControllerConfig{
		KubeClientSet:        f.fixture.kubeclient,
		ArgoProjClientset:    f.fixture.client,
		AnalysisRunInformer:  f.argo.Argoproj().V1alpha1().AnalysisRuns(),
		RolloutInformer:      f.argo.Argoproj().V1alpha1().Rollouts(),
		JobInformer:          f.kube.Batch().V1().Jobs(),
		JobPodsInformer:      f.kube.Core().V1().Pods(),
		AnalysisRunWorkQueue: queue,
		Recorder:             record.NewFakeEventRecorder(),
		MetricsServer:        metrics.NewMetricsServer(metrics.ServerConfig{K8SRequestProvider: &metrics.K8sRequestsCountProvider{}}),
		TracingEnabled:       true,
		TraceHandoffs:        f.controller.traceHandoffs,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- controller.Run(ctx, 1) }()
	t.Cleanup(func() {
		cancel()
		queue.ShutDown()
		require.NoError(t, <-done)
	})
	queue.Add(rollout.Namespace + "/" + run.Name)
	// Deliver the watch update after the producing handler publishes its result.
	require.Eventually(t, func() bool { return len(f.recorder.Ended()) == 2 }, 5*time.Second, 10*time.Millisecond)
	terminal, err := f.fixture.client.ArgoprojV1alpha1().AnalysisRuns(run.Namespace).Get(context.Background(), run.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, f.phase, terminal.Status.Phase)
	require.NoError(t, f.argo.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Update(terminal))
	result := f.recorder.Ended()[1]
	assert.Equal(t, "controller.AnalysisRun", result.Name())
	assert.Equal(t, codes.Unset, result.Status().Code)
	require.Equal(t, result.SpanContext(), f.controller.traceHandoffs.ResultParent(rollout, terminal, time.Now()))
	return terminal, result
}

func (f *controllerTraceFlow) consumeGate(t *testing.T, rollout *v1alpha1.Rollout, run *v1alpha1.AnalysisRun, creator, result sdktrace.ReadOnlySpan) {
	t.Helper()
	writeErr := errors.New("consumer status write failed")
	failNext := true
	f.fixture.client.PrependReactor("patch", "rollouts", func(clienttesting.Action) (bool, runtime.Object, error) {
		if failNext {
			failNext = false
			return true, nil, writeErr
		}
		return false, nil, nil
	})
	require.ErrorIs(t, f.controller.syncHandler(context.Background(), "default/checkout"), writeErr)
	assert.Equal(t, result.SpanContext(), f.controller.traceHandoffs.ResultParent(rollout, run, time.Now()), "a failed write retains the result")
	require.NoError(t, f.controller.syncHandler(context.Background(), "default/checkout"))
	accepted, err := f.fixture.client.ArgoprojV1alpha1().Rollouts(rollout.Namespace).Get(context.Background(), rollout.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, f.phase == v1alpha1.AnalysisPhaseFailed, accepted.Status.Abort)
	if f.phase == v1alpha1.AnalysisPhaseSuccessful {
		require.NotNil(t, accepted.Status.CurrentStepIndex)
		assert.EqualValues(t, 1, *accepted.Status.CurrentStepIndex)
	}
	assert.False(t, f.controller.traceHandoffs.ResultParent(rollout, run, time.Now()).IsValid(), "an accepted write consumes the result")
	spans := f.recorder.Ended()
	require.Len(t, spans, 4)
	assert.Equal(t, creator.SpanContext().SpanID(), result.Parent().SpanID())
	assert.Equal(t, codes.Error, spans[2].Status().Code)
	assert.Equal(t, codes.Unset, spans[3].Status().Code)
	ids := make(map[string]bool)
	for _, span := range spans {
		assert.Equal(t, creator.SpanContext().TraceID(), span.SpanContext().TraceID())
		ids[span.SpanContext().SpanID().String()] = true
	}
	assert.Len(t, ids, 4)
	assert.Equal(t, result.SpanContext().SpanID(), spans[2].Parent().SpanID())
	assert.Equal(t, result.SpanContext().SpanID(), spans[3].Parent().SpanID())
}

func controllerTraceVersionedPatch(f *fixture) clienttesting.ReactionFunc {
	return func(action clienttesting.Action) (bool, runtime.Object, error) {
		patch := action.(clienttesting.PatchAction)
		current, err := f.client.Tracker().Get(patch.GetResource(), patch.GetNamespace(), patch.GetName())
		if err != nil {
			return true, nil, err
		}
		original, err := json.Marshal(current)
		if err != nil {
			return true, nil, err
		}
		modified, err := jsonpatch.MergePatch(original, patch.GetPatch())
		if err != nil {
			return true, nil, err
		}
		updated := current.DeepCopyObject()
		if err := json.Unmarshal(modified, updated); err != nil {
			return true, nil, err
		}
		metadata, err := meta.Accessor(updated)
		if err != nil {
			return true, nil, err
		}
		version, err := strconv.Atoi(metadata.GetResourceVersion())
		if err != nil {
			return true, nil, err
		}
		metadata.SetResourceVersion(strconv.Itoa(version + 1))
		return true, updated, f.client.Tracker().Update(patch.GetResource(), updated, patch.GetNamespace())
	}
}
