package testdata

import (
	"os"
	"path"
	"runtime"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

var testDir string

func init() {
	_, filename, _, _ := runtime.Caller(0)
	// The ".." may change depending on you folder structure
	testDir = path.Join(path.Dir(filename))
}

type RolloutObjects struct {
	Rollouts     []*v1alpha1.Rollout
	ReplicaSets  []*appsv1.ReplicaSet
	Pods         []*corev1.Pod
	Experiments  []*v1alpha1.Experiment
	AnalysisRuns []*v1alpha1.AnalysisRun
	Deployments  []*appsv1.Deployment
}

func (r *RolloutObjects) AllObjects() []k8sruntime.Object {
	var objs []k8sruntime.Object
	for _, o := range r.Rollouts {
		objs = append(objs, o)
	}
	for _, o := range r.ReplicaSets {
		objs = append(objs, o)
	}
	for _, o := range r.Deployments {
		objs = append(objs, o)
	}
	for _, o := range r.Pods {
		objs = append(objs, o)
	}
	for _, o := range r.Experiments {
		objs = append(objs, o)
	}
	for _, o := range r.AnalysisRuns {
		objs = append(objs, o)
	}
	return objs
}

func NewCanaryRollout() *RolloutObjects {
	return discoverObjects(testDir + "/canary")
}

func NewBlueGreenRollout() *RolloutObjects {
	return discoverObjects(testDir + "/blue-green")
}

func NewExperimentAnalysisRollout() *RolloutObjects {
	return discoverObjects(testDir + "/experiment-analysis")
}

func NewExperimentAnalysisJobRollout() *RolloutObjects {
	return discoverObjects(testDir + "/experiment-step")
}

func NewInvalidRollout() *RolloutObjects {
	return discoverObjects(testDir + "/rollout-invalid")
}

func NewAbortedRollout() *RolloutObjects {
	return discoverObjects(testDir + "/rollout-aborted")
}

func discoverObjects(path string) *RolloutObjects {
	files, err := os.ReadDir(path)
	if err != nil {
		panic(err)
	}
	// we set creation timestamp so that AGE output in CLI can be compared
	aWeekAgo := metav1.NewTime(time.Now().Add(-7 * 24 * time.Hour).Truncate(time.Second))

	var objs RolloutObjects
	for _, file := range files {
		yamlBytes, err := os.ReadFile(path + "/" + file.Name())
		if err != nil {
			panic(err)
		}
		typeMeta := GetTypeMeta(yamlBytes)
		switch typeMeta.Kind {
		case "Rollout":
			var ro v1alpha1.Rollout
			err = yaml.UnmarshalStrict(yamlBytes, &ro, yaml.DisallowUnknownFields)
			if err != nil {
				panic(err)
			}
			ro.CreationTimestamp = aWeekAgo
			objs.Rollouts = append(objs.Rollouts, &ro)
		case "ReplicaSet":
			var rs appsv1.ReplicaSet
			err = yaml.UnmarshalStrict(yamlBytes, &rs, yaml.DisallowUnknownFields)
			if err != nil {
				panic(err)
			}
			rs.CreationTimestamp = aWeekAgo
			objs.ReplicaSets = append(objs.ReplicaSets, &rs)
		case "Deployment":
			var de appsv1.Deployment
			err = yaml.UnmarshalStrict(yamlBytes, &de, yaml.DisallowUnknownFields)
			if err != nil {
				panic(err)
			}
			de.CreationTimestamp = aWeekAgo
			objs.Deployments = append(objs.Deployments, &de)
		case "Pod":
			var pod corev1.Pod
			err = yaml.UnmarshalStrict(yamlBytes, &pod, yaml.DisallowUnknownFields)
			if err != nil {
				panic(err)
			}
			pod.CreationTimestamp = aWeekAgo
			objs.Pods = append(objs.Pods, &pod)
		case "Experiment":
			var exp v1alpha1.Experiment
			err = yaml.UnmarshalStrict(yamlBytes, &exp, yaml.DisallowUnknownFields)
			if err != nil {
				panic(err)
			}
			exp.CreationTimestamp = aWeekAgo
			objs.Experiments = append(objs.Experiments, &exp)
		case "AnalysisRun":
			var run v1alpha1.AnalysisRun
			err = yaml.UnmarshalStrict(yamlBytes, &run, yaml.DisallowUnknownFields)
			if err != nil {
				panic(err)
			}
			run.CreationTimestamp = aWeekAgo
			for i, m := range run.Status.MetricResults[0].Measurements {
				m.StartedAt = &aWeekAgo
				m.FinishedAt = &aWeekAgo
				run.Status.MetricResults[0].Measurements[i] = m
			}
			objs.AnalysisRuns = append(objs.AnalysisRuns, &run)
		}
	}
	return &objs
}

func GetTypeMeta(yamlBytes []byte) metav1.TypeMeta {
	type k8sObj struct {
		metav1.TypeMeta `json:",inline"`
	}
	var o k8sObj
	err := yaml.Unmarshal(yamlBytes, &o)
	if err != nil {
		panic(err)
	}
	return o.TypeMeta
}
