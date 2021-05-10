package record

import (
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
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
	// PrometheusCounter is an optional prometheus counter to increment upon recording the event
	PrometheusCounter *prometheus.CounterVec
}

type EventRecorder interface {
	Eventf(object runtime.Object, opts EventOptions, messageFmt string, args ...interface{}) error
	Warnf(object runtime.Object, opts EventOptions, messageFmt string, args ...interface{}) error
	K8sRecorder() record.EventRecorder
}

// EventRecorderAdapter implements the EventRecorder interface
type EventRecorderAdapter struct {
	// Recorder is a K8s EventRecorder
	Recorder record.EventRecorder
}

func NewEventRecorder(kubeclientset kubernetes.Interface) EventRecorder {
	// Create event broadcaster
	// Add argo-rollouts custom resources to the default Kubernetes Scheme so Events can be
	// logged for argo-rollouts types.
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	return &EventRecorderAdapter{
		Recorder: recorder,
	}
}

func NewFakeEventRecorder() EventRecorder {
	return &EventRecorderAdapter{
		Recorder: &record.FakeRecorder{},
	}
}

func (e *EventRecorderAdapter) Eventf(object runtime.Object, opts EventOptions, messageFmt string, args ...interface{}) error {
	if opts.EventType == "" {
		opts.EventType = corev1.EventTypeNormal
	}
	return e.eventf(object, opts.EventType == corev1.EventTypeWarning, opts, messageFmt, args...)
}

func (e *EventRecorderAdapter) Warnf(object runtime.Object, opts EventOptions, messageFmt string, args ...interface{}) error {
	opts.EventType = corev1.EventTypeWarning
	return e.eventf(object, true, opts, messageFmt, args...)
}

func (e *EventRecorderAdapter) eventf(object runtime.Object, warn bool, opts EventOptions, messageFmt string, args ...interface{}) error {
	logCtx := logutil.WithObject(object)
	if opts.EventReason != "" {
		logCtx = logCtx.WithField("event_reason", opts.EventReason)
		e.Recorder.Eventf(object, opts.EventType, opts.EventReason, messageFmt, args...)
	}
	logFn := logCtx.Infof
	if warn {
		logFn = logCtx.Warnf
	}
	logFn(messageFmt, args...)

	if opts.PrometheusCounter != nil {
		objectMeta, err := meta.Accessor(object)
		if err != nil {
			return err
		}
		opts.PrometheusCounter.WithLabelValues(objectMeta.GetNamespace(), objectMeta.GetName()).Inc()
	}
	return nil
}

func (e *EventRecorderAdapter) K8sRecorder() record.EventRecorder {
	return e.Recorder
}
