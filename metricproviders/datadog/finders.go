package datadog

import (
	"context"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type CredentialsFinder interface {
	FindCredentials(logCtx log.Entry) (string, string, string)
}

func NewSecretFinder(kubeclientset kubernetes.Interface, secretName string, namespace string) *secretFinder {
	return &secretFinder{
		kubeclientset: kubeclientset,
		secretName:    secretName,
		namespace:     namespace,
	}
}

type secretFinder struct {
	kubeclientset kubernetes.Interface
	secretName    string
	namespace     string
}

func (sf *secretFinder) FindCredentials(logCtx log.Entry) (string, string, string) {
	address := ""
	secret, err := sf.kubeclientset.CoreV1().Secrets(sf.namespace).Get(context.TODO(), sf.secretName, metav1.GetOptions{})
	if err != nil {
		logCtx.Debugf("secret %s in namespace %s", sf.namespace, sf.secretName)
		return "", "", ""
	}
	apiKey := string(secret.Data[DatadogApiKey])
	appKey := string(secret.Data[DatadogAppKey])
	if _, hasAddress := secret.Data[DatadogAddress]; hasAddress {
		address = string(secret.Data[DatadogAddress])
	}
	return address, apiKey, appKey
}

type envVariablesFinder struct {
}

func NewEnvVariablesFinder() *envVariablesFinder {
	return &envVariablesFinder{}
}
func (evf *envVariablesFinder) FindCredentials(logCtx log.Entry) (string, string, string) {
	secretKeys := []string{DatadogApiKey, DatadogAppKey, DatadogAddress}
	envValuesByKey := lookupKeysInEnv(secretKeys)
	if len(envValuesByKey) == len(secretKeys) {
		return envValuesByKey[DatadogAddress], envValuesByKey[DatadogApiKey], envValuesByKey[DatadogAppKey]
	}
	logCtx.Debug("credentials not found as env variables")
	return "", "", ""
}

func lookupKeysInEnv(keys []string) map[string]string {
	valuesByKey := make(map[string]string)
	for i := range keys {
		key := keys[i]
		formattedKey := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		if value, ok := os.LookupEnv(fmt.Sprintf("DD_%s", formattedKey)); ok {
			valuesByKey[key] = value
		}
	}
	return valuesByKey
}
