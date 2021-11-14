package istio

import (
	"context"
	"errors"
	"fmt"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	PrimaryClusterSecretLabel = "istio.argoproj.io/primary-cluster"
)

func GetPrimaryClusterDynamicClient(kubeClient kubernetes.Interface, namespace string) (string, dynamic.Interface) {
	primaryClusterSecret := getPrimaryClusterSecret(kubeClient, namespace)
	if primaryClusterSecret != nil {
		clusterId, clientConfig, err := getKubeClientConfig(primaryClusterSecret)
		if err != nil {
			return clusterId, nil
		}

		config, err := clientConfig.ClientConfig()
		if err != nil {
			log.Errorf("Error fetching primary ClientConfig: %v", err)
			return clusterId, nil
		}

		dynamicClient, err := dynamic.NewForConfig(config)
		if err != nil {
			log.Errorf("Error building dynamic client from config: %v", err)
			return clusterId, nil
		}

		return clusterId, dynamicClient
	}

	return "", nil
}

func getPrimaryClusterSecret(kubeClient kubernetes.Interface, namespace string) *corev1.Secret {
	req, err := labels.NewRequirement(PrimaryClusterSecretLabel, selection.Equals, []string{"true"})
	if err != nil {
		return nil
	}

	secrets, err := kubeClient.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{Limit: 1, LabelSelector: req.String()})
	if err != nil {
		return nil
	}

	if secrets != nil && len(secrets.Items) > 0 {
		return &secrets.Items[0]
	}

	return nil
}

func getKubeClientConfig(secret *corev1.Secret) (string, clientcmd.ClientConfig, error) {
	for clusterId, kubeConfig := range secret.Data {
		primaryClusterConfig, err := buildKubeClientConfig(kubeConfig)
		if err != nil {
			log.Errorf("Error building kubeconfig for primary cluster %s: %v", clusterId, err)
			return clusterId, nil, fmt.Errorf("error building primary cluster client %s: %v", clusterId, err)
		}
		log.Infof("Istio primary/config cluster is %s", clusterId)
		return clusterId, primaryClusterConfig, err
	}
	return "", nil, nil
}

func buildKubeClientConfig(kubeConfig []byte) (clientcmd.ClientConfig, error) {
	if len(kubeConfig) == 0 {
		return nil, errors.New("kubeconfig is empty")
	}

	rawConfig, err := clientcmd.Load(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("kubeconfig cannot be loaded: %v", err)
	}

	if err := clientcmd.Validate(*rawConfig); err != nil {
		return nil, fmt.Errorf("kubeconfig is not valid: %v", err)
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*rawConfig, &clientcmd.ConfigOverrides{})
	return clientConfig, nil
}
