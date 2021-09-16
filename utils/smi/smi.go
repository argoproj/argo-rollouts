package smi

import (
	"context"

	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DoesSMIExist(smiClient smiclientset.Interface, namespace string) bool {
	_, err := smiClient.SplitV1alpha1().TrafficSplits(namespace).List(context.TODO(), metav1.ListOptions{Limit: 1})
	if err != nil {
		return false
	}
	return true
}
