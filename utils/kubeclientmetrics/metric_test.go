package kubeclientmetrics

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type fakeWrapper struct {
	t             *testing.T
	currentCount  int
	expectedCount int
}

func (f fakeWrapper) RoundTrip(r *http.Request) (*http.Response, error) {
	resp := httptest.NewRecorder()
	resp.Code = 201
	assert.Equal(f.t, f.currentCount, f.expectedCount)
	return resp.Result(), nil
}

// TestWrappingTwice Ensures that the config doesn't lose any previous wrappers and the previous wrapper
// gets executed first
func TestAddMetricsTransportWrapperWrapTwice(t *testing.T) {
	config := &rest.Config{
		Host: "",
	}
	currentCount := 0
	config.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		return fakeWrapper{
			t:             t,
			expectedCount: 0,
			currentCount:  currentCount,
		}
	}

	newConfig := AddMetricsTransportWrapper(config, func(info ResourceInfo) {
		currentCount++
	})

	client := kubernetes.NewForConfigOrDie(newConfig)
	client.AppsV1().ReplicaSets(metav1.NamespaceDefault).Get("test", metav1.GetOptions{})
	// Ensures second wrapper added by AddMetricsTransportWrapper is executed
	assert.Equal(t, 1, currentCount)

}

func TestResolveK8sRequestVerb(t *testing.T) {
	regex := regexp.MustCompile(findPathRegex)
	m := metricsRoundTripper{
		processPath: regex,
	}

	request := func(str string) *http.Request {
		requestURL, err := url.Parse(str)
		if err != nil {
			panic(err)
		}
		return &http.Request{
			Method: "GET",
			URL:    requestURL,
		}
	}
	t.Run("Pod LIST", func(t *testing.T) {
		r := request("https://127.0.0.1/api/v1/namespaces/default/pods")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, List, verb)
	})
	t.Run("Pod GET", func(t *testing.T) {
		r := request("https://127.0.0.1/api/v1/namespaces/default/pods/pod-name-123456")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, Get, verb)
	})
	t.Run("Namespace LIST", func(t *testing.T) {
		r := request("https://127.0.0.1/api/v1/namespaces")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, List, verb)
	})
	t.Run("Namespace GET", func(t *testing.T) {
		r := request("https://127.0.0.1/api/v1/namespaces/default")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, Get, verb)
	})
	t.Run("ReplicaSet LIST", func(t *testing.T) {
		r := request("https://127.0.0.1/apis/extensions/v1beta1/namespaces/default/replicasets")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, List, verb)
	})
	t.Run("ReplicaSet GET", func(t *testing.T) {
		r := request("https://127.0.0.1/apis/extensions/v1beta1/namespaces/default/replicasets/rs-abc123")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, Get, verb)
	})
	t.Run("VirtualService LIST", func(t *testing.T) {
		r := request("https://127.0.0.1/apis/networking.istio.io/v1alpha3/namespaces/default/virtualservices")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, List, verb)
	})
	t.Run("VirtualService GET", func(t *testing.T) {
		r := request("https://127.0.0.1/apis/networking.istio.io/v1alpha3/namespaces/default/virtualservices/virtual-service")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, Get, verb)
	})
	t.Run("ClusterRole LIST", func(t *testing.T) {
		r := request("https://127.0.0.1/apis/rbac.authorization.k8s.io/v1/clusterroles")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, List, verb)
	})
	t.Run("ClusterRole GET", func(t *testing.T) {
		r := request("https://127.0.0.1/apis/rbac.authorization.k8s.io/v1/clusterroles/argo-rollouts-clusterrole")
		verb := m.resolveK8sRequestVerb(r)
		assert.Equal(t, Get, verb)
	})
}

func TestGetRequest(t *testing.T) {
	expectedStatusCode := 201
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(expectedStatusCode)
	}))
	defer ts.Close()
	executed := false
	config := &rest.Config{
		Host: ts.URL,
	}
	newConfig := AddMetricsTransportWrapper(config, func(info ResourceInfo) {
		assert.Equal(t, expectedStatusCode, info.StatusCode)
		assert.Equal(t, "replicasets", info.Kind)
		assert.Equal(t, metav1.NamespaceDefault, info.Namespace)
		assert.Equal(t, "test", info.Name)
		assert.Equal(t, Get, info.Verb)
		executed = true
	})
	client := kubernetes.NewForConfigOrDie(newConfig)
	client.AppsV1().ReplicaSets(metav1.NamespaceDefault).Get("test", metav1.GetOptions{})
	assert.True(t, executed)
}

func TestListRequest(t *testing.T) {
	expectedStatusCode := 201
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(expectedStatusCode)
	}))
	defer ts.Close()
	executed := false
	config := &rest.Config{
		Host: ts.URL,
	}
	newConfig := AddMetricsTransportWrapper(config, func(info ResourceInfo) {
		assert.Equal(t, expectedStatusCode, info.StatusCode)
		assert.Equal(t, "replicasets", info.Kind)
		assert.Equal(t, metav1.NamespaceDefault, info.Namespace)
		assert.Equal(t, "", info.Name)
		assert.Equal(t, List, info.Verb)
		executed = true
	})
	client := kubernetes.NewForConfigOrDie(newConfig)
	client.AppsV1().ReplicaSets(metav1.NamespaceDefault).List(metav1.ListOptions{})
	assert.True(t, executed)
}

func TestCreateRequest(t *testing.T) {
	expectedStatusCode := 201
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(expectedStatusCode)
	}))
	defer ts.Close()
	executed := false
	config := &rest.Config{
		Host: ts.URL,
	}
	newConfig := AddMetricsTransportWrapper(config, func(info ResourceInfo) {
		assert.Equal(t, expectedStatusCode, info.StatusCode)
		assert.Equal(t, "replicasets", info.Kind)
		assert.Equal(t, metav1.NamespaceDefault, info.Namespace)
		assert.Equal(t, "test", info.Name)
		assert.Equal(t, Create, info.Verb)
		executed = true
	})
	client := kubernetes.NewForConfigOrDie(newConfig)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: metav1.NamespaceDefault,
		},
	}
	client.AppsV1().ReplicaSets(metav1.NamespaceDefault).Create(rs)
	assert.True(t, executed)
}

func TestDeleteRequest(t *testing.T) {
	expectedStatusCode := 201
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(expectedStatusCode)
	}))
	defer ts.Close()
	executed := false
	config := &rest.Config{
		Host: ts.URL,
	}
	newConfig := AddMetricsTransportWrapper(config, func(info ResourceInfo) {
		assert.Equal(t, expectedStatusCode, info.StatusCode)
		assert.Equal(t, "replicasets", info.Kind)
		assert.Equal(t, metav1.NamespaceDefault, info.Namespace)
		assert.Equal(t, "test", info.Name)
		assert.Equal(t, Delete, info.Verb)
		executed = true
	})
	client := kubernetes.NewForConfigOrDie(newConfig)
	client.AppsV1().ReplicaSets(metav1.NamespaceDefault).Delete("test", &metav1.DeleteOptions{})
	assert.True(t, executed)
}

func TestPatchRequest(t *testing.T) {
	expectedStatusCode := 201
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(expectedStatusCode)
	}))
	defer ts.Close()
	executed := false
	config := &rest.Config{
		Host: ts.URL,
	}
	newConfig := AddMetricsTransportWrapper(config, func(info ResourceInfo) {
		assert.Equal(t, expectedStatusCode, info.StatusCode)
		assert.Equal(t, "replicasets", info.Kind)
		assert.Equal(t, metav1.NamespaceDefault, info.Namespace)
		assert.Equal(t, "test", info.Name)
		assert.Equal(t, Patch, info.Verb)
		executed = true
	})
	client := kubernetes.NewForConfigOrDie(newConfig)
	client.AppsV1().ReplicaSets(metav1.NamespaceDefault).Patch("test", types.MergePatchType, []byte("{}"))
	assert.True(t, executed)
}

func TestUpdateRequest(t *testing.T) {
	expectedStatusCode := 201
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(expectedStatusCode)
	}))
	defer ts.Close()
	executed := false
	config := &rest.Config{
		Host: ts.URL,
	}
	newConfig := AddMetricsTransportWrapper(config, func(info ResourceInfo) {
		assert.Equal(t, expectedStatusCode, info.StatusCode)
		assert.Equal(t, "replicasets", info.Kind)
		assert.Equal(t, metav1.NamespaceDefault, info.Namespace)
		assert.Equal(t, "test", info.Name)
		assert.Equal(t, Update, info.Verb)
		executed = true
	})
	client := kubernetes.NewForConfigOrDie(newConfig)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	client.AppsV1().ReplicaSets(metav1.NamespaceDefault).Update(rs)
	assert.True(t, executed)
}

func TestUnknownRequest(t *testing.T) {
	expectedStatusCode := 201
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(expectedStatusCode)
	}))
	defer ts.Close()
	executed := false
	config := &rest.Config{
		Host: ts.URL,
	}
	newConfig := AddMetricsTransportWrapper(config, func(info ResourceInfo) {
		assert.Equal(t, expectedStatusCode, info.StatusCode)
		assert.Equal(t, Unknown, info.Verb)
		executed = true
	})
	client := kubernetes.NewForConfigOrDie(newConfig)
	client.Discovery().RESTClient().Verb("invalid-verb").Do()
	assert.True(t, executed)
}
