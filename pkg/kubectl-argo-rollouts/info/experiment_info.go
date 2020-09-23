package info

import (
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type ExperimentInfo struct {
	metav1.TypeMeta `json:",inline"`
	Metadata        `json:"metadata,omitempty"`

	Spec ExperimentInfoSpec `json:"spec"`
}

type ExperimentInfoSpec struct {
	Icon         string            `json:"-"`
	Revision     int               `json:"revision"`
	Status       string            `json:"status"`
	Message      string            `json:"message,omitempty"`
	ReplicaSets  []ReplicaSetInfo  `json:"replicaSets,omitempty"`
	AnalysisRuns []AnalysisRunInfo `json:"analysisRuns,omitempty"`
}

func NewExperimentInfo(
	exp *v1alpha1.Experiment,
	allReplicaSets []*appsv1.ReplicaSet,
	allAnalysisRuns []*v1alpha1.AnalysisRun,
	allPods []*corev1.Pod,
) *ExperimentInfo {
	expInfo := ExperimentInfo{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ExperimentInfo",
			APIVersion: "argoproj.io/v1alpha1",
		},
		Metadata: Metadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:              exp.Name,
				Namespace:         exp.Namespace,
				CreationTimestamp: exp.CreationTimestamp,
				UID:               exp.UID,
			},
		},
		Spec: ExperimentInfoSpec{
			Status:       string(exp.Status.Phase),
			Message:      exp.Status.Message,
			Icon:         analysisIcon(exp.Status.Phase),
			Revision:     parseRevision(exp.ObjectMeta.Annotations),
			ReplicaSets:  getReplicaSetInfo(exp.UID, nil, allReplicaSets, allPods),
			AnalysisRuns: getAnalysisRunInfo(exp.UID, allAnalysisRuns),
		},
	}
	return &expInfo
}

func getExperimentInfo(
	ro *v1alpha1.Rollout,
	allExperiments []*v1alpha1.Experiment,
	allReplicaSets []*appsv1.ReplicaSet,
	allAnalysisRuns []*v1alpha1.AnalysisRun,
	allPods []*corev1.Pod,
) []ExperimentInfo {

	var expInfos []ExperimentInfo
	for _, exp := range allExperiments {
		if ownerRef(exp.OwnerReferences, []types.UID{ro.UID}) == nil {
			continue
		}
		expInfo := NewExperimentInfo(exp, allReplicaSets, allAnalysisRuns, allPods)
		expInfos = append(expInfos, *expInfo)
	}
	sort.Slice(expInfos[:], func(i, j int) bool {
		if expInfos[i].Spec.Revision > expInfos[j].Spec.Revision {
			return true
		}
		return expInfos[i].CreationTimestamp.Before(&expInfos[j].CreationTimestamp)
	})
	return expInfos
}

// Images returns a list of images that are currently running along with tags on which stack they belong to
func (r *ExperimentInfoSpec) Images() []ImageInfo {
	var images []ImageInfo
	for _, rsInfo := range r.ReplicaSets {
		if rsInfo.Spec.Replicas > 0 {
			for _, image := range rsInfo.Spec.Images {
				newImage := ImageInfo{
					Image: image,
				}
				if rsInfo.Spec.Template != "" {
					newImage.Tags = append(newImage.Tags, fmt.Sprintf("Σ:%s", rsInfo.Spec.Template))
				}
				images = mergeImageAndTags(newImage, images)
			}
		}
	}
	return images
}

func analysisIcon(status v1alpha1.AnalysisPhase) string {
	switch status {
	case v1alpha1.AnalysisPhaseSuccessful:
		return IconOK
	case v1alpha1.AnalysisPhaseInconclusive:
		return IconUnknown
	case v1alpha1.AnalysisPhaseFailed:
		return IconBad
	case v1alpha1.AnalysisPhaseError:
		return IconWarning
	case v1alpha1.AnalysisPhaseRunning:
		return IconProgressing
	case v1alpha1.AnalysisPhasePending:
		return IconWaiting
	}
	return " "
}

func (r *ExperimentInfo) DeepCopyObject() runtime.Object {
	out := new(ExperimentInfo)
	*out = *r
	out.TypeMeta = r.TypeMeta
	return out
}
