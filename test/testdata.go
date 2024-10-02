package test

import (
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/common"
)

const (
	FakeArgoRolloutsNamespace = "fake-argo-rollouts-ns"
	FakeDestNamespace   = "fake-dest-ns"
	FakeClusterURL      = "https://fake-cluster:443"
)

func NewFakeConfigMap() *apiv1.ConfigMap {
	cm := apiv1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoRolloutsConfigMapName,
			Namespace: FakeArgoRolloutsNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "argo-rollouts",
			},
		},
		Data: make(map[string]string),
	}
	return &cm
}

func NewFakeSecret(policy ...string) *apiv1.Secret {
	secret := apiv1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoRolloutsSecretName,
			Namespace: FakeArgoRolloutsNamespace,
		},
		Data: map[string][]byte{
			"admin.password":   []byte("test"),
			"server.secretkey": []byte("test"),
		},
	}
	return &secret
}

func NewInMemoryRedis() (*redis.Client, func()) {
	mr, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr.Close
}
