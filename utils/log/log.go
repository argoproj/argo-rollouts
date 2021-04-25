package log

import (
	"strings"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

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

// WithObject returns a logging context for an object which includes <kind>=<name> and namespace=<namespace>
func WithObject(obj runtime.Object) *log.Entry {
	logCtx := log.NewEntry(log.StandardLogger())
	gvk := obj.GetObjectKind().GroupVersionKind()
	objectMeta, err := meta.Accessor(obj)
	if err == nil {
		logCtx = logCtx.WithField("namespace", objectMeta.GetNamespace())
		logCtx = logCtx.WithField(strings.ToLower(gvk.Kind), objectMeta.GetName())
	}
	return logCtx
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
