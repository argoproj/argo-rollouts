package record

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/argoproj/notifications-engine/pkg/services"

	"github.com/argoproj/notifications-engine/pkg/api"
	"github.com/argoproj/notifications-engine/pkg/subscriptions"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	controllerAgentName   = "rollouts-controller"
	NotificationConfigMap = "argo-rollouts-notification-configmap"
	NotificationSecret    = "argo-rollouts-notification-secret"
)

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

	eventf func(object runtime.Object, warn bool, opts EventOptions, messageFmt string, args ...interface{})
	// apiFactory is a notifications engine API factory
	apiFactory api.Factory
}

func NewEventRecorder(kubeclientset kubernetes.Interface, rolloutEventCounter *prometheus.CounterVec, apiFactory api.Factory) EventRecorder {
	// Create event broadcaster
	// Add argo-rollouts custom resources to the default Kubernetes Scheme so Events can be
	// logged for argo-rollouts types.
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	k8srecorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	recorder := &EventRecorderAdapter{
		Recorder:            k8srecorder,
		RolloutEventCounter: rolloutEventCounter,
		apiFactory:          apiFactory,
	}
	recorder.eventf = recorder.defaultEventf
	return recorder
}

// FakeEventRecorder wraps EventRecorderAdapter but with a convenience function to get all the event
// reasons which were emitted
type FakeEventRecorder struct {
	EventRecorderAdapter
	Events []string
}

func NewFakeEventRecorder() *FakeEventRecorder {
	recorder := NewEventRecorder(
		k8sfake.NewSimpleClientset(),
		prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rollout_events_total",
			},
			[]string{"name", "namespace", "type", "reason"},
		),
		nil,
	).(*EventRecorderAdapter)
	recorder.Recorder = record.NewFakeRecorder(1000)
	fakeRecorder := &FakeEventRecorder{}
	recorder.eventf = func(object runtime.Object, warn bool, opts EventOptions, messageFmt string, args ...interface{}) {
		recorder.defaultEventf(object, warn, opts, messageFmt, args...)
		fakeRecorder.Events = append(fakeRecorder.Events, opts.EventReason)
	}
	fakeRecorder.EventRecorderAdapter = *recorder
	return fakeRecorder
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

// defaultEventf is the default implementation of eventf, which is able to be overwritten for
// test purposes
func (e *EventRecorderAdapter) defaultEventf(object runtime.Object, warn bool, opts EventOptions, messageFmt string, args ...interface{}) {
	logCtx := logutil.WithObject(object)

	if opts.EventReason != "" {
		logCtx = logCtx.WithField("event_reason", opts.EventReason)
		e.Recorder.Eventf(object, opts.EventType, opts.EventReason, messageFmt, args...)

		// Increment rollout_events_total counter
		kind, namespace, name := logutil.KindNamespaceName(logCtx)
		if kind == "Rollout" {
			e.RolloutEventCounter.WithLabelValues(namespace, name, opts.EventType, opts.EventReason).Inc()
		}
		err := e.sendNotifications(object, opts)
		if err != nil {
			logCtx.Errorf("Notifications failed to send for eventReason %s with error: %s", opts.EventReason, err)
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

func NewAPIFactorySettings() api.Settings {
	return api.Settings{
		SecretName:    NotificationSecret,
		ConfigMapName: NotificationConfigMap,
		InitGetVars: func(cfg *api.Config, configMap *corev1.ConfigMap, secret *corev1.Secret) (api.GetVars, error) {
			return func(obj map[string]interface{}, dest services.Destination) map[string]interface{} {
				return map[string]interface{}{"rollout": obj}
			}, nil
		},
	}
}

// Send notifications for triggered event if user is subscribed
func (e *EventRecorderAdapter) sendNotifications(object runtime.Object, opts EventOptions) error {
	subsFromAnnotations := subscriptions.Annotations(object.(metav1.Object).GetAnnotations())
	destByTrigger := subsFromAnnotations.GetDestinations(nil, map[string][]string{})

	trigger := translateReasonToTrigger(opts.EventReason)

	destinations := destByTrigger[trigger]
	if len(destinations) == 0 {
		return nil
	}

	notificationsAPI, err := e.apiFactory.GetAPI()
	if err != nil {
		return err
	}

	// Creates config for notifications for built-in triggers
	triggerActions, ok := notificationsAPI.GetConfig().Triggers[trigger]
	if !ok {
		return nil
	}

	objBytes, err := json.Marshal(object)
	if err != nil {
		return err
	}
	var objMap map[string]interface{}
	err = json.Unmarshal(objBytes, &objMap)
	if err != nil {
		return err
	}
	for _, dest := range destinations {
		err = notificationsAPI.Send(objMap, triggerActions[0].Send, dest)
		if err != nil {
			log.Errorf("notification error: %s", err.Error())
			return err
		}
	}
	return nil
}

func translateReasonToTrigger(reason string) string {
	var matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
	var matchAllCap = regexp.MustCompile("([a-z0-9])([A-Z])")
	trigger := matchFirstCap.ReplaceAllString(reason, "${1}-${2}")
	trigger = matchAllCap.ReplaceAllString(trigger, "${1}-${2}")
	return "on-" + strings.ToLower(trigger)
}
