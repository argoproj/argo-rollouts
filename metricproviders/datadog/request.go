package datadog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	metadataHTTPStatusCode   = "httpStatusCode"
	metadataRequestOutcome   = "requestOutcome"
	metadataResponseReceived = "responseReceived"

	requestOutcomeHTTPError         = "http_error"
	requestOutcomeResponseError     = "response_error"
	requestOutcomeResponseReadError = "response_read_error"
	requestOutcomeSuccess           = "success"
	requestOutcomeTimeout           = "timeout"
	requestOutcomeTransportError    = "transport_error"

	defaultRequestTimeout = 10 * time.Second
)

type responseReadError struct {
	err error
}

func (e *responseReadError) Error() string {
	return fmt.Sprintf("Failed to read Datadog API response body: %v", e.err)
}

func (e *responseReadError) Unwrap() error {
	return e.err
}

func newHTTPClient(dd *v1alpha1.DatadogMetric) (*http.Client, error) {
	timeout := defaultRequestTimeout
	if dd.RequestTimeout != "" {
		var err error
		timeout, err = dd.RequestTimeout.Duration()
		if err != nil {
			return nil, err
		}
	}
	return &http.Client{Timeout: timeout}, nil
}

func (p *Provider) sendRequest(request *http.Request) (*http.Response, map[string]string, error) {
	response, err := p.client.Do(request)
	if err != nil {
		outcome := requestOutcomeTransportError
		message := "Datadog API request failed before receiving an HTTP response"
		if isTimeoutError(err) {
			outcome = requestOutcomeTimeout
			message = fmt.Sprintf("Datadog API request timed out after %s before receiving an HTTP response", p.client.Timeout)
		}
		return nil, map[string]string{
			metadataRequestOutcome:   outcome,
			metadataResponseReceived: strconv.FormatBool(false),
		}, fmt.Errorf("%s: %w", message, err)
	}

	return response, map[string]string{
		metadataHTTPStatusCode:   strconv.Itoa(response.StatusCode),
		metadataResponseReceived: strconv.FormatBool(true),
	}, nil
}

func responseErrorOutcome(statusCode int, err error) string {
	var readErr *responseReadError
	if errors.As(err, &readErr) {
		return requestOutcomeResponseReadError
	}
	if statusCode != http.StatusOK {
		return requestOutcomeHTTPError
	}
	return requestOutcomeResponseError
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
