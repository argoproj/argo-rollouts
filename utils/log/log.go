package log

import (
	"flag"
	"strconv"
	"strings"

	"github.com/bombsimon/logrusr/v4"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	// RolloutKey defines the key for the rollout field
	RolloutKey = "rollout"
	// ExperimentKey defines the key for the experiment field
	ExperimentKey = "experiment"
	// AnalysisRunKey defines the key for the analysisrun field
	AnalysisRunKey = "analysisrun"
	// ServiceKey defines the key for the service field
	ServiceKey = "service"
	// IngressKey defines the key for the ingress field
	IngressKey = "ingress"
	// NamespaceKey defines the key for the namespace field
	NamespaceKey = "namespace"
)

// SetKLogLogger set the klog logger for the k8s go-client
func SetKLogLogger(logger *log.Logger) {
	klog.SetLogger(logrusr.New(logger))
}

// SetKLogLevel set the klog level for the k8s go-client
func SetKLogLevel(klogLevel int) {
	klog.InitFlags(nil)
	_ = flag.Set("logtostderr", "true")
	_ = flag.Set("v", strconv.Itoa(klogLevel))
}

// WithObject returns a logging context for an object which includes <kind>=<name> and namespace=<namespace>
func WithObject(obj runtime.Object) *log.Entry {
	logCtx := log.NewEntry(log.StandardLogger())
	gvk := obj.GetObjectKind().GroupVersionKind()
	kind := gvk.Kind
	if kind == "" {
		// it's possible for kind can be empty
		switch obj.(type) {
		case *v1alpha1.Rollout:
			kind = "rollout"
		case *v1alpha1.AnalysisRun:
			kind = "analysisrun"
		case *v1alpha1.AnalysisTemplate:
			kind = "analysistemplate"
		case *v1alpha1.ClusterAnalysisTemplate:
			kind = "clusteranalysistemplate"
		case *v1alpha1.Experiment:
			kind = "experiment"
		}
	}
	objectMeta, err := meta.Accessor(obj)
	if err == nil {
		logCtx = logCtx.WithField("namespace", objectMeta.GetNamespace())
		logCtx = logCtx.WithField(strings.ToLower(kind), objectMeta.GetName())
	}
	return logCtx
}

// KindNamespaceName is a helper to get kind, namespace, name from a logging context
// This is an optimization that callers can use to avoid inferring this again from a runtime.Object
func KindNamespaceName(logCtx *log.Entry) (string, string, string) {
	var kind string
	var nameIf interface{}
	var ok bool
	if nameIf, ok = logCtx.Data["rollout"]; ok {
		kind = "Rollout"
	} else if nameIf, ok = logCtx.Data["analysisrun"]; ok {
		kind = "AnalysisRun"
	} else if nameIf, ok = logCtx.Data["analysistemplate"]; ok {
		kind = "AnalysisTemplate"
	} else if nameIf, ok = logCtx.Data["experiment"]; ok {
		kind = "Experiment"
	} else if nameIf, ok = logCtx.Data["clusteranalysistemplate"]; ok {
		kind = "ClusterAnalysisTemplate"
	}
	name, _ := nameIf.(string)
	namespace, _ := logCtx.Data["namespace"].(string)
	return kind, namespace, name
}

// WithRollout returns a logging context for Rollouts
func WithRollout(rollout *v1alpha1.Rollout) *log.Entry {
	return log.WithField(RolloutKey, rollout.Name).WithField(NamespaceKey, rollout.Namespace)
}

// WithExperiment returns a logging context for Experiments
func WithExperiment(experiment *v1alpha1.Experiment) *log.Entry {
	return log.WithField(ExperimentKey, experiment.Name).WithField(NamespaceKey, experiment.Namespace)
}

// WithAnalysisRun returns a logging context for AnalysisRun
func WithAnalysisRun(ar *v1alpha1.AnalysisRun) *log.Entry {
	return log.WithField(AnalysisRunKey, ar.Name).WithField(NamespaceKey, ar.Namespace)
}

// WithRedactor returns a log entry with the inputted secret values redacted
func WithRedactor(entry log.Entry, secrets []string) *log.Entry {
	newFormatter := RedactorFormatter{
		entry.Logger.Formatter,
		secrets,
	}
	entry.Logger.SetFormatter(&newFormatter)
	return &entry
}

func WithVersionFields(entry *log.Entry, r *v1alpha1.Rollout) *log.Entry {
	return entry.WithFields(map[string]interface{}{
		"resourceVersion": r.ResourceVersion,
		"generation":      r.Generation,
	})
}
