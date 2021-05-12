package record

import (
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/scheme"

	rolloutscheme "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/scheme"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func init() {
	// Add argo-rollouts custom resources to the default Kubernetes Scheme so Events can be
	// logged for argo-rollouts types.
	utilruntime.Must(rolloutscheme.AddToScheme(scheme.Scheme))
}

const controllerAgentName = "rollouts-controller"

type EventOptions struct {
	// EventType is the kubernetes event type (Normal or Warning). Defaults to Normal
	EventType string
	// EventReason is a Kubernetes EventReason of why this event is generated.
	// Reason should be short and unique; it  should be in UpperCamelCase format (starting with a
	// capital letter). "reason" will be used to automate handling of events, so imagine people
	// writing switch statements to handle them.
	EventReason string
}

type EventRecorder interface {
	Eventf(object runtime.Object, opts EventOptions, messageFmt string, args ...interface{})
	Warnf(object runtime.Object, opts EventOptions, messageFmt string, args ...interface{})
	K8sRecorder() record.EventRecorder
}

// EventRecorderAdapter implements the EventRecorder interface
type EventRecorderAdapter struct {
	// Recorder is a K8s EventRecorder
	Recorder record.EventRecorder
	// RolloutEventCounter is a counter to increment on events
	RolloutEventCounter *prometheus.CounterVec
}

func NewEventRecorder(kubeclientset kubernetes.Interface, rolloutEventCounter *prometheus.CounterVec) EventRecorder {
	// Create event broadcaster
	// Add argo-rollouts custom resources to the default Kubernetes Scheme so Events can be
	// logged for argo-rollouts types.
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	return &EventRecorderAdapter{
		Recorder:            recorder,
		RolloutEventCounter: rolloutEventCounter,
	}
}

func NewFakeEventRecorder() EventRecorder {
	return NewEventRecorder(
		k8sfake.NewSimpleClientset(),
		prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rollout_events_total",
			},
			[]string{"name", "namespace", "type", "reason"},
		),
	)
}

func (e *EventRecorderAdapter) Eventf(object runtime.Object, opts EventOptions, messageFmt string, args ...interface{}) {
	if opts.EventType == "" {
		opts.EventType = corev1.EventTypeNormal
	}
	e.eventf(object, opts.EventType == corev1.EventTypeWarning, opts, messageFmt, args...)
}

func (e *EventRecorderAdapter) Warnf(object runtime.Object, opts EventOptions, messageFmt string, args ...interface{}) {
	opts.EventType = corev1.EventTypeWarning
	e.eventf(object, true, opts, messageFmt, args...)
}

func (e *EventRecorderAdapter) eventf(object runtime.Object, warn bool, opts EventOptions, messageFmt string, args ...interface{}) {
	logCtx := logutil.WithObject(object)

	if opts.EventReason != "" {
		logCtx = logCtx.WithField("event_reason", opts.EventReason)
		e.Recorder.Eventf(object, opts.EventType, opts.EventReason, messageFmt, args...)

		// Increment rollout_events_total counter
		kind, namespace, name := logutil.KindNamespaceName(logCtx)
		if kind == "Rollout" {
			e.RolloutEventCounter.WithLabelValues(namespace, name, opts.EventType, opts.EventReason).Inc()
		}
	}

	logFn := logCtx.Infof
	if warn {
		logFn = logCtx.Warnf
	}
	logFn(messageFmt, args...)
}

func (e *EventRecorderAdapter) K8sRecorder() record.EventRecorder {
	return e.Recorder
}
