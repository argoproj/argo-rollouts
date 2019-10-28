package info

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	IconProgressing = "◷"
	IconWarning     = "⚠"
	IconUnknown     = "?"
	IconOK          = "✔"
	IconBad         = "✖"
	IconPaused      = "‖"
	IconNeutral     = "•"
)

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
