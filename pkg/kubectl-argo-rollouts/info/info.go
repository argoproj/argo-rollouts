package info

import (
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/duration"

	"math"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	IconWaiting     = "◷"
	IconProgressing = "◌"
	IconWarning     = "⚠"
	IconUnknown     = "?"
	IconOK          = "✔"
	IconBad         = "✖"
	IconPaused      = "॥"
	IconNeutral     = "•"
)

const (
	InfoTagCanary  = "canary"
	InfoTagStable  = "stable"
	InfoTagActive  = "active"
	InfoTagPreview = "preview"
	InfoTagPing    = "ping"
	InfoTagPong    = "pong"
)

type Metadata v1.ObjectMeta

type ImageInfo struct {
	Image string
	Tags  []string
}

func Age(m v1.ObjectMeta) string {
	return duration.HumanDuration(timeutil.MetaNow().Sub(m.CreationTimestamp.Time))
}

func ownerRef(ownerRefs []metav1.OwnerReference, uids []types.UID) *metav1.OwnerReference {
	for _, ownerRef := range ownerRefs {
		for _, uid := range uids {
			if ownerRef.UID == uid {
				return &ownerRef
			}
		}
	}
	return nil
}

func parseRevision(annotations_ map[string]string) int32 {
	const DefaultAllocate int32 = 256
	if annotations_ != nil {
		revision, err := strconv.Atoi(annotations_[annotations.RevisionAnnotation])

		if err != nil {
			return DefaultAllocate
		}
		if revision > 0 && revision <= math.MaxInt32 {
			return (int32)(revision)
		}
	}
	return DefaultAllocate

}

func parseExperimentTemplateName(annotations_ map[string]string) string {
	if annotations_ != nil {
		return annotations_[v1alpha1.ExperimentTemplateNameAnnotationKey]
	}
	return ""
}

func parseScaleDownDeadline(annotations_ map[string]string) string {
	if annotations_ != nil {
		return annotations_[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]
	}
	return ""
}
