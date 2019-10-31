package info

import (
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/argoproj/argo-rollouts/utils/annotations"
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
)

type Metadata struct {
	Name              string
	UID               types.UID
	CreationTimestamp metav1.Time
}

type ImageInfo struct {
	Image string
	Tags  []string
}

func (m Metadata) Age() string {
	return duration.HumanDuration(metav1.Now().Sub(m.CreationTimestamp.Time))
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

func parseRevision(annots map[string]string) int {
	if annots != nil {
		if revision, err := strconv.Atoi(annots[annotations.RevisionAnnotation]); err == nil {
			return revision
		}
	}
	return 0
}
