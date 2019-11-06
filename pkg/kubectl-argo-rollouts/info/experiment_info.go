package info

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type ExperimentInfo struct {
	Metadata
	Icon         string
	Revision     int
	Status       string
	Message      string
	ReplicaSets  []ReplicaSetInfo
	AnalysisRuns []AnalysisRunInfo
}

func NewExperimentInfo(
	exp *v1alpha1.Experiment,
	allReplicaSets []*appsv1.ReplicaSet,
	allAnalysisRuns []*v1alpha1.AnalysisRun,
	allPods []*corev1.Pod,
) *ExperimentInfo {

	expInfo := ExperimentInfo{
		Metadata: Metadata{
			Name:              exp.Name,
			Namespace:         exp.Namespace,
			CreationTimestamp: exp.CreationTimestamp,
			UID:               exp.UID,
		},
		Status:  string(exp.Status.Status),
		Message: exp.Status.Message,
	}
	expInfo.Icon = analysisIcon(exp.Status.Status)
	expInfo.Revision = parseRevision(exp.ObjectMeta.Annotations)
	expInfo.ReplicaSets = getReplicaSetInfo(exp.UID, nil, allReplicaSets, allPods)
	expInfo.AnalysisRuns = getAnalysisRunInfo(exp.UID, allAnalysisRuns)
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
		if expInfos[i].Revision > expInfos[j].Revision {
			return true
		}
		return expInfos[i].CreationTimestamp.Before(&expInfos[j].CreationTimestamp)
	})
	return expInfos
}

// Images returns a list of images that are currently running along with tags on which stack they belong to
func (r *ExperimentInfo) Images() []ImageInfo {
	uniqueImages := make(map[string]ImageInfo)
	appendIfMissing := func(image string, infoTag string) {
		if _, ok := uniqueImages[image]; !ok {
			uniqueImages[image] = ImageInfo{
				Image: image,
			}
		}
		if infoTag != "" {
			doAppend := true
			for _, existingTag := range uniqueImages[image].Tags {
				if existingTag == infoTag {
					doAppend = false
					break
				}
			}
			if doAppend {
				newInfo := uniqueImages[image]
				newInfo.Tags = append(uniqueImages[image].Tags, infoTag)
				uniqueImages[image] = newInfo
			}
		}
	}
	for _, rsInfo := range r.ReplicaSets {
		if rsInfo.Replicas > 0 {
			for _, image := range rsInfo.Images {
				appendIfMissing(image, "")
			}
		}
	}

	var images []ImageInfo
	for _, v := range uniqueImages {
		images = append(images, v)
	}

	sort.Slice(images[:], func(i, j int) bool {
		return images[i].Image < images[j].Image
	})
	return images
}

func analysisIcon(status v1alpha1.AnalysisStatus) string {
	switch status {
	case v1alpha1.AnalysisStatusSuccessful:
		return IconOK
	case v1alpha1.AnalysisStatusInconclusive:
		return IconUnknown
	case v1alpha1.AnalysisStatusFailed:
		return IconBad
	case v1alpha1.AnalysisStatusError:
		return IconWarning
	case v1alpha1.AnalysisStatusRunning:
		return IconProgressing
	case v1alpha1.AnalysisStatusPending:
		return IconWaiting
	}
	return " "
}
