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

type PrimaryCluster interface {
	GetKubeClient() kubernetes.Interface
	GetDynamicClient() dynamic.Interface
}

type primaryCluster struct {
	namespace string
	secret *corev1.Secret
	kubeClient kubernetes.Interface
	dynamicClient dynamic.Interface
}

func NewPrimaryCluster(kubeClient kubernetes.Interface, dynamicClient dynamic.Interface, namespace string) PrimaryCluster {
	pc := &primaryCluster{namespace: namespace, kubeClient: kubeClient, dynamicClient: dynamicClient}

	primaryClusterSecret := getPrimaryClusterSecret(kubeClient, namespace)
	if primaryClusterSecret != nil {
		pc.secret = primaryClusterSecret
		clientConfig, err := getKubeClientConfig(primaryClusterSecret)
		if err != nil {
			// TODO log the error
			return pc
		}

		config, err := clientConfig.ClientConfig()
		if err != nil {
			// TODO log the error
			return pc
		}

		kubeClient, err := kubernetes.NewForConfig(config)
		if err != nil {
			// TODO log the error
			return pc
		}

		dynamicClient, err := dynamic.NewForConfig(config)
		if err != nil {
			// TODO log the error
			return pc
		}

		pc.kubeClient = kubeClient
		pc.dynamicClient = dynamicClient
	}

	return pc
}

func (pc *primaryCluster) GetKubeClient() kubernetes.Interface {
	return pc.kubeClient
}

func (pc *primaryCluster) GetDynamicClient() dynamic.Interface {
	return pc.dynamicClient
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

func getKubeClientConfig(secret *corev1.Secret) (clientcmd.ClientConfig, error) {
	for clusterId, kubeConfig := range secret.Data {
		primaryClusterConfig, err := buildKubeClientConfig(kubeConfig)
		if err != nil {
			// TODO log error
			continue
		}
		log.Infof("Istio primary/config cluster is %s", clusterId)
		return primaryClusterConfig, err
	}
	return nil, nil
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