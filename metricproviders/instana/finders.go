package instana

import (
	"context"
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// credentialsFinder abstracts credential retrieval so we can test without a real cluster
type credentialsFinder interface {
	findCredentials(logCtx log.Entry) (endpoint, apiToken string)
}

// ---------------------------------------------------------------------------
// Kubernetes Secret finder
// ---------------------------------------------------------------------------

type secretFinder struct {
	kubeclientset kubernetes.Interface
	secretName    string
	namespace     string
}

func newSecretFinder(kubeclientset kubernetes.Interface, secretName, namespace string) *secretFinder {
	return &secretFinder{
		kubeclientset: kubeclientset,
		secretName:    secretName,
		namespace:     namespace,
	}
}

func (sf *secretFinder) findCredentials(logCtx log.Entry) (string, string) {
	secret, err := sf.kubeclientset.CoreV1().Secrets(sf.namespace).Get(context.TODO(), sf.secretName, metav1.GetOptions{})
	if err != nil {
		logCtx.Debugf("instana: error searching for secret %s in namespace %s: %s", sf.secretName, sf.namespace, err.Error())
		return "", ""
	}

	endpoint := string(secret.Data[InstanaAddress])
	apiToken := string(secret.Data[InstanaAPIToken])

	if endpoint == "" || apiToken == "" {
		logCtx.Debugf("instana: credentials missing in secret %s/%s", sf.namespace, sf.secretName)
		return "", ""
	}

	return endpoint, apiToken
}

// ---------------------------------------------------------------------------
// Environment variable finder
// ---------------------------------------------------------------------------

type envVarFinder struct{}

func newEnvVarFinder() *envVarFinder {
	return &envVarFinder{}
}

func (evf *envVarFinder) findCredentials(logCtx log.Entry) (string, string) {
	keys := map[string]string{
		InstanaAddress:  fmt.Sprintf("INSTANA_%s", envKey(InstanaAddress)),
		InstanaAPIToken: fmt.Sprintf("INSTANA_%s", envKey(InstanaAPIToken)),
	}

	endpoint, endpointOK := os.LookupEnv(keys[InstanaAddress])
	apiToken, apiTokenOK := os.LookupEnv(keys[InstanaAPIToken])

	if endpointOK && apiTokenOK {
		return endpoint, apiToken
	}

	logCtx.Debug("instana: credentials not found in environment variables")
	return "", ""
}

// envKey converts a secret key (e.g. "api-token") to an env-var suffix (e.g. "API_TOKEN")
func envKey(key string) string {
	result := ""
	for _, ch := range key {
		if ch == '-' {
			result += "_"
		} else if ch >= 'a' && ch <= 'z' {
			result += string(ch - 32)
		} else {
			result += string(ch)
		}
	}
	return result
}
