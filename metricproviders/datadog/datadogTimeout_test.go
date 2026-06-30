package datadog

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// TestRequestTimeout verifies that the configurable RequestTimeout on the Datadog
// metric is honored by the HTTP client: a short timeout against a slow server fails,
// while a generous timeout against the same server succeeds.
func TestRequestTimeout(t *testing.T) {
	const expectedApiKey = "0123456789abcdef0123456789abcdef"
	const expectedAppKey = "0123456789abcdef0123456789abcdef01234567"
	const serverDelay = 200 * time.Millisecond

	newProvider := func(requestTimeout v1alpha1.DurationString) v1alpha1.MetricProvider {
		return v1alpha1.MetricProvider{
			Datadog: &v1alpha1.DatadogMetric{
				Interval:       "5m",
				Query:          "avg:kubernetes.cpu.user.total{*}",
				ApiVersion:     "v2",
				RequestTimeout: requestTimeout,
			},
		}
	}

	tests := []struct {
		name           string
		requestTimeout v1alpha1.DurationString
		expectedPhase  v1alpha1.AnalysisPhase
	}{
		{
			name:           "short timeout against slow server fails",
			requestTimeout: "10ms",
			expectedPhase:  v1alpha1.AnalysisPhaseError,
		},
		{
			name:           "generous timeout against slow server succeeds",
			requestTimeout: "5s",
			expectedPhase:  v1alpha1.AnalysisPhaseSuccessful,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				time.Sleep(serverDelay)
				rw.Header().Set("Content-Type", "application/json")
				io.WriteString(rw, `{"data": {"attributes": {"columns": [ {"values": [0.0006332881882246533]}]}}}`)
			}))
			defer server.Close()

			tokenSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: DatadogTokensSecretName,
				},
				Data: map[string][]byte{
					"address": []byte(server.URL),
					"api-key": []byte(expectedApiKey),
					"app-key": []byte(expectedAppKey),
				},
			}

			metric := v1alpha1.Metric{
				Name:             "timeout test",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         newProvider(test.requestTimeout),
			}

			logCtx := log.WithField("test", "test")
			fakeClient := k8sfake.NewSimpleClientset()
			fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, tokenSecret, nil
			})

			provider, _ := NewDatadogProvider(*logCtx, fakeClient, "namespace", metric)
			measurement := provider.Run(newAnalysisRun(), metric)

			assert.Equal(t, test.expectedPhase, measurement.Phase)
			if test.expectedPhase == v1alpha1.AnalysisPhaseError {
				assert.Contains(t, measurement.Message, "Client.Timeout")
			}
		})
	}
}
