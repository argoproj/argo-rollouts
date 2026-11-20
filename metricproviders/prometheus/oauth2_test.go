package prometheus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestOAuth2MeasurementTimeout(t *testing.T) {
	for _, test := range []struct {
		name        string
		partialBody bool
		timeout     time.Duration
	}{
		{name: "token response headers", timeout: time.Second},
		{name: "token response body", partialBody: true, timeout: 300 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			var tokenRequests, queries atomic.Int32
			tokenServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
				io.Copy(io.Discard, r.Body)
				tokenRequests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if test.partialBody {
					io.WriteString(w, `{"access_token":`)
					w.(http.Flusher).Flush()
				}
				if !waitOAuth2TestRequest(r, release) {
					return
				}
				if test.partialBody {
					io.WriteString(w, `"token","token_type":"Bearer","expires_in":3600}`)
				} else {
					writeOAuth2TestToken(w, "token", 3600)
				}
			})
			queryServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, _ <-chan struct{}) {
				queries.Add(1)
				writeOAuth2TestQuery(w)
			})
			metric := oauth2TestMetric(tokenServer.URL, queryServer.URL)
			provider := newOAuth2TestProvider(t, metric)
			if test.partialBody {
				provider.timeout = test.timeout
			}
			// The headers case uses the configured one-second metric timeout.
			result := make(chan v1alpha1.Measurement, 1)
			go func() { result <- provider.Run(newAnalysisRun(), metric) }()
			select {
			case measurement := <-result:
				if measurement.Phase != v1alpha1.AnalysisPhaseError || !strings.Contains(measurement.Message, context.DeadlineExceeded.Error()) {
					t.Fatalf("expected a measurement deadline error, got %#v", measurement)
				}
				if measurement.StartedAt == nil || measurement.FinishedAt == nil {
					t.Fatalf("measurement timestamps were not recorded: %#v", measurement)
				}
			case <-time.After(test.timeout + time.Second):
				t.Fatal("measurement timeout did not cancel the stalled OAuth2 token request")
			}
			if tokenRequests.Load() != 1 || queries.Load() != 0 {
				t.Fatalf("expected one token request and no query, got tokens=%d queries=%d", tokenRequests.Load(), queries.Load())
			}
		})
	}
}

func TestOAuth2TokenAndQueryShareDeadline(t *testing.T) {
	const stageDelay = 600 * time.Millisecond
	var tokenRequests, queries atomic.Int32
	tokenServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		io.Copy(io.Discard, r.Body)
		tokenRequests.Add(1)
		select {
		case <-time.After(stageDelay):
			writeOAuth2TestToken(w, "token", 3600)
		case <-r.Context().Done():
		case <-release:
		}
	})
	queryServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		io.Copy(io.Discard, r.Body)
		queries.Add(1)
		select {
		case <-time.After(stageDelay):
			writeOAuth2TestQuery(w)
		case <-r.Context().Done():
		case <-release:
		}
	})
	api := newOAuth2TestAPI(t, oauth2TestMetric(tokenServer.URL, queryServer.URL))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := awaitOAuth2TestQuery(t, startOAuth2TestQuery(api, ctx), 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("token exchange and query must share the caller's deadline, got %v", err)
	}
	if tokenRequests.Load() != 1 || queries.Load() != 1 {
		t.Fatalf("expected the shared budget to expire during the query, got tokens=%d queries=%d", tokenRequests.Load(), queries.Load())
	}
}

func TestOAuth2TokenReuseAcrossMeasurements(t *testing.T) {
	for _, test := range []struct {
		name         string
		expiresIn    int
		bodyAuthOnly bool
		wantRequests int32
		wantTokens   int32
	}{
		{name: "valid cached token", expiresIn: 3600, wantRequests: 1, wantTokens: 1},
		// A token inside oauth2's early-expiry window is refreshed without sleeping.
		{name: "short-lived token", expiresIn: 1, wantRequests: 2, wantTokens: 2},
		{name: "remember body authentication on refresh", expiresIn: 1, bodyAuthOnly: true, wantRequests: 3, wantTokens: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var tokenRequests, issuedTokens atomic.Int32
			tokenServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, _ <-chan struct{}) {
				tokenRequests.Add(1)
				if err := r.ParseForm(); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if test.bodyAuthOnly && r.Header.Get("Authorization") != "" {
					http.Error(w, "use credentials in the request body", http.StatusUnauthorized)
					return
				}
				clientID, secret, ok := r.BasicAuth()
				if !ok {
					clientID, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
				}
				if clientID != "client" || secret != "secret" || r.Form.Get("scope") != "metrics.read" || r.Form.Get("grant_type") != "client_credentials" {
					http.Error(w, "incorrect client credentials grant", http.StatusBadRequest)
					return
				}
				writeOAuth2TestToken(w, fmt.Sprintf("token-%d", issuedTokens.Add(1)), test.expiresIn)
			})
			authorizations := make(chan string, 2)
			queryServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, _ <-chan struct{}) {
				authorizations <- r.Header.Get("Authorization")
				writeOAuth2TestQuery(w)
			})
			metric := oauth2TestMetric(tokenServer.URL, queryServer.URL)
			provider := newOAuth2TestProvider(t, metric)
			for i := int32(1); i <= 2; i++ {
				// Run cancels its context before returning. The next measurement must
				// still reuse a valid token or refresh using its own context.
				measurement := provider.Run(newAnalysisRun(), metric)
				if measurement.Phase != v1alpha1.AnalysisPhaseSuccessful {
					t.Fatalf("measurement %d failed: %#v", i, measurement)
				}
				wantToken := i
				if test.wantTokens == 1 {
					wantToken = 1
				}
				if authorization := <-authorizations; authorization != fmt.Sprintf("Bearer token-%d", wantToken) {
					t.Fatalf("measurement %d used unexpected authorization %q", i, authorization)
				}
			}
			if tokenRequests.Load() != test.wantRequests || issuedTokens.Load() != test.wantTokens {
				t.Fatalf("unexpected token reuse: requests=%d tokens=%d, want requests=%d tokens=%d", tokenRequests.Load(), issuedTokens.Load(), test.wantRequests, test.wantTokens)
			}
		})
	}
}

func TestOAuth2MeasurementRecoversAfterTokenTimeout(t *testing.T) {
	var tokenRequests, queries atomic.Int32
	tokenServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		io.Copy(io.Discard, r.Body)
		if tokenRequests.Add(1) == 1 {
			if !waitOAuth2TestRequest(r, release) {
				return
			}
		}
		writeOAuth2TestToken(w, "token", 3600)
	})
	queryServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, _ <-chan struct{}) {
		queries.Add(1)
		writeOAuth2TestQuery(w)
	})
	metric := oauth2TestMetric(tokenServer.URL, queryServer.URL)
	provider := newOAuth2TestProvider(t, metric)
	provider.timeout = 300 * time.Millisecond
	result := make(chan v1alpha1.Measurement, 1)
	go func() { result <- provider.Run(newAnalysisRun(), metric) }()
	select {
	case measurement := <-result:
		if measurement.Phase != v1alpha1.AnalysisPhaseError || !strings.Contains(measurement.Message, context.DeadlineExceeded.Error()) {
			t.Fatalf("expected the first token request to time out, got %#v", measurement)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("token acquisition ignored the first measurement's timeout")
	}
	if queries.Load() != 0 {
		t.Fatalf("timed-out token acquisition sent %d queries", queries.Load())
	}
	provider.timeout = time.Second
	if measurement := provider.Run(newAnalysisRun(), metric); measurement.Phase != v1alpha1.AnalysisPhaseSuccessful {
		t.Fatalf("a new measurement could not recover after token timeout: %#v", measurement)
	}
	if tokenRequests.Load() != 2 || queries.Load() != 1 {
		t.Fatalf("expected a fresh token request and one query after recovery, got tokens=%d queries=%d", tokenRequests.Load(), queries.Load())
	}
}

func TestOAuth2AuthenticationProbeSharesDeadline(t *testing.T) {
	var tokenRequests, completedTokens, queries atomic.Int32
	bodyRequest := make(chan struct{}, 1)
	tokenServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		tokenRequests.Add(1)
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, _, basic := r.BasicAuth(); basic {
			select {
			case <-time.After(600 * time.Millisecond):
				http.Error(w, "use credentials in the request body", http.StatusUnauthorized)
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		if r.Form.Get("client_id") != "client" || r.Form.Get("client_secret") != "secret" {
			http.Error(w, "missing credentials in body", http.StatusBadRequest)
			return
		}
		bodyRequest <- struct{}{}
		select {
		case <-time.After(600 * time.Millisecond):
			completedTokens.Add(1)
			writeOAuth2TestToken(w, "token", 3600)
		case <-r.Context().Done():
		case <-release:
		}
	})
	queryServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, _ <-chan struct{}) {
		queries.Add(1)
		writeOAuth2TestQuery(w)
	})
	api := newOAuth2TestAPI(t, oauth2TestMetric(tokenServer.URL, queryServer.URL))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := startOAuth2TestQuery(api, ctx)
	select {
	case <-bodyRequest:
	case <-time.After(2 * time.Second):
		t.Fatal("OAuth2 did not retry authentication with credentials in the body")
	}
	err := awaitOAuth2TestQuery(t, result, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected both authentication attempts to share the deadline, got %v", err)
	}
	if tokenRequests.Load() != 2 || completedTokens.Load() != 0 || queries.Load() != 0 {
		t.Fatalf("the total budget must expire before a token or query completes, got attempts=%d tokens=%d queries=%d", tokenRequests.Load(), completedTokens.Load(), queries.Load())
	}
}

func TestOAuth2ConcurrentRefreshWaiterCanCancel(t *testing.T) {
	var tokenRequests, queries atomic.Int32
	refreshStarted := make(chan struct{}, 1)
	tokenServer, finishRefresh := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		io.Copy(io.Discard, r.Body)
		tokenRequests.Add(1)
		select {
		case refreshStarted <- struct{}{}:
		default:
		}
		if waitOAuth2TestRequest(r, release) {
			writeOAuth2TestToken(w, "token", 3600)
		}
	})
	queryServer, _ := newOAuth2TestServer(t, func(w http.ResponseWriter, r *http.Request, _ <-chan struct{}) {
		queries.Add(1)
		writeOAuth2TestQuery(w)
	})
	api := newOAuth2TestAPI(t, oauth2TestMetric(tokenServer.URL, queryServer.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := startOAuth2TestQuery(api, ctx)
	select {
	case <-refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("the first request did not start token acquisition")
	}
	waiterCtx, cancelWaiter := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelWaiter()
	waiter := startOAuth2TestQuery(api, waiterCtx)
	other := startOAuth2TestQuery(api, ctx)
	// The first request is still refreshing. Waiting for its lock must not
	// prevent an independent measurement from honoring its own timeout.
	err := awaitOAuth2TestQuery(t, waiter, time.Second)
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("refresh waiter did not honor its deadline: %v", err)
	}
	if tokenRequests.Load() != 1 || queries.Load() != 0 {
		t.Fatalf("concurrent requests should share one pending token exchange, got tokens=%d queries=%d", tokenRequests.Load(), queries.Load())
	}
	finishRefresh()
	for _, result := range []<-chan error{first, other} {
		if err := awaitOAuth2TestQuery(t, result, time.Second); err != nil {
			t.Fatalf("live request failed after the shared token exchange completed: %v", err)
		}
	}
	if tokenRequests.Load() != 1 || queries.Load() != 2 {
		t.Fatalf("expected one token exchange and two live queries, got tokens=%d queries=%d", tokenRequests.Load(), queries.Load())
	}
}

func TestOAuth2PreservesTLSAndHeaders(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		t.Run(fmt.Sprintf("insecure=%t", insecure), func(t *testing.T) {
			var tokenRequests, queries atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Metrics-Tenant") != "tenant" {
					http.Error(w, "missing custom header", http.StatusBadRequest)
					return
				}
				if r.URL.Path == "/token" {
					io.Copy(io.Discard, r.Body)
					tokenRequests.Add(1)
					writeOAuth2TestToken(w, "token", 3600)
					return
				}
				queries.Add(1)
				if r.Header.Get("Authorization") != "Bearer token" {
					http.Error(w, "missing bearer token", http.StatusUnauthorized)
					return
				}
				writeOAuth2TestQuery(w)
			}))
			t.Cleanup(server.Close)
			metric := oauth2TestMetric(server.URL+"/token", server.URL)
			metric.Provider.Prometheus.Insecure = insecure
			metric.Provider.Prometheus.Headers = []v1alpha1.WebMetricHeader{{Key: "X-Metrics-Tenant", Value: "tenant"}}
			measurement := newOAuth2TestProvider(t, metric).Run(newAnalysisRun(), metric)
			if insecure {
				if measurement.Phase != v1alpha1.AnalysisPhaseSuccessful || tokenRequests.Load() != 1 || queries.Load() != 1 {
					t.Fatalf("insecure TLS or headers were not preserved for token and query requests: measurement=%#v tokens=%d queries=%d", measurement, tokenRequests.Load(), queries.Load())
				}
			} else if measurement.Phase != v1alpha1.AnalysisPhaseError || !strings.Contains(measurement.Message, "certificate") || tokenRequests.Load() != 0 || queries.Load() != 0 {
				t.Fatalf("secure TLS should reject the token server's self-signed certificate: measurement=%#v tokens=%d queries=%d", measurement, tokenRequests.Load(), queries.Load())
			}
		})
	}
}

func startOAuth2TestQuery(api v1.API, ctx context.Context) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, _, err := api.Query(ctx, "up", time.Now())
		result <- err
	}()
	return result
}

func awaitOAuth2TestQuery(t *testing.T, result <-chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(timeout):
		t.Fatal("OAuth2 query did not return within its deadline")
		return nil
	}
}

func oauth2TestMetric(tokenURL, queryURL string) v1alpha1.Metric {
	timeout := int64(1)
	return v1alpha1.Metric{
		Name:             "oauth2-test",
		SuccessCondition: "result[0] == 1",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Address: queryURL,
				Query:   "up",
				Timeout: &timeout,
				Authentication: v1alpha1.Authentication{
					OAuth2: v1alpha1.OAuth2Config{
						TokenURL:     tokenURL,
						ClientID:     "client",
						ClientSecret: "secret",
						Scopes:       []string{"metrics.read"},
					},
				},
			},
		},
	}
}

func newOAuth2TestAPI(t *testing.T, metric v1alpha1.Metric) v1.API {
	t.Helper()
	api, err := NewPrometheusAPI(metric)
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func newOAuth2TestProvider(t *testing.T, metric v1alpha1.Metric) *Provider {
	t.Helper()
	provider, err := NewPrometheusProvider(newOAuth2TestAPI(t, metric), *log.NewEntry(log.New()), metric)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func newOAuth2TestServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, <-chan struct{})) (*httptest.Server, func()) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, release)
	}))
	// Unblock handlers before Close, including when testing the broken implementation.
	t.Cleanup(func() {
		unblock()
		server.Close()
	})
	return server, unblock
}

func waitOAuth2TestRequest(r *http.Request, release <-chan struct{}) bool {
	select {
	case <-r.Context().Done():
		return false
	case <-release:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

func writeOAuth2TestToken(w http.ResponseWriter, token string, expiresIn int) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":%d}`, token, expiresIn)
}

func writeOAuth2TestQuery(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[0,"1"]}]}}`)
}
