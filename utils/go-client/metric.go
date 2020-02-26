package go_client

import (
	"io/ioutil"
	"net/http"
	"path"
	"strings"

	"github.com/prometheus/common/log"
	"k8s.io/client-go/rest"

	"github.com/argoproj/argo-rollouts/utils/unstructured"
)

type K8sRequestVerb string

const (
	List    K8sRequestVerb = "List"
	Get     K8sRequestVerb = "Get"
	Create  K8sRequestVerb = "Create"
	Update  K8sRequestVerb = "Update"
	Patch   K8sRequestVerb = "Patch"
	Delete  K8sRequestVerb = "Delete"
	Unknown K8sRequestVerb = "Unknown"
)

type ResourceInfo struct {
	Kind       string
	Namespace  string
	Name       string
	Verb       K8sRequestVerb
	StatusCode int
}

func (ri ResourceInfo) HasAllFields() bool {
	return ri.Kind != "" && ri.Namespace != "" && ri.Name != "" && ri.Verb != "" && ri.StatusCode != 0
}

type metricsRoundTripper struct {
	roundTripper    http.RoundTripper
	inc             func(ResourceInfo) error
	commonResources map[string]bool
}

func (m metricsRoundTripper) resolveK8sRequestVerb(r *http.Request) K8sRequestVerb {
	if r.Method == "POST" {
		return Create
	}
	if r.Method == "DELETE" {
		return Delete
	}
	if r.Method == "PATCH" {
		return Patch
	}
	if r.Method == "PUT" {
		return Update
	}
	if r.Method == "GET" {
		resource := path.Base(r.URL.Path)
		if _, ok := m.commonResources[resource]; ok {
			return List
		}
		return Get
	}
	return Unknown
}

func handleList(r *http.Request, statusCode int) ResourceInfo {
	path := strings.Split(r.URL.Path, "/")
	len := len(path)
	kind := path[len-1]
	namespace := path[len-2]
	return ResourceInfo{
		Kind:       kind,
		Namespace:  namespace,
		Verb:       List,
		StatusCode: statusCode,
	}
}

func handleGet(r *http.Request, statusCode int) ResourceInfo {
	path := strings.Split(r.URL.Path, "/")
	len := len(path)
	name := path[len-1]
	kind := path[len-2]
	namespace := path[len-3]
	return ResourceInfo{
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
		Verb:       Get,
		StatusCode: statusCode,
	}
}

func handleCreate(r *http.Request, statusCode int) ResourceInfo {
	kind := path.Base(r.URL.Path)
	bodyIO, err := r.GetBody()
	if err != nil {
		log.With("Kind", kind).Warn("Unable to Process Create request")
		return ResourceInfo{}
	}
	body, err := ioutil.ReadAll(bodyIO)
	if err != nil {
		log.With("Kind", kind).Warn("Unable to Process Create request")
		return ResourceInfo{}
	}
	obj, err := unstructured.StrToUnstructured(string(body))
	if err != nil {
		log.With("Kind", kind).Warn("Unable to Process Create request")
		return ResourceInfo{}
	}
	return ResourceInfo{
		Kind:       kind,
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
		Verb:       Create,
		StatusCode: statusCode,
	}
}

func handleDelete(r *http.Request, statusCode int) ResourceInfo {
	path := strings.Split(r.URL.Path, "/")
	len := len(path)
	name := path[len-1]
	kind := path[len-2]
	namespace := path[len-3]
	return ResourceInfo{
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
		Verb:       Delete,
		StatusCode: statusCode,
	}
}

func handlePatch(r *http.Request, statusCode int) ResourceInfo {
	path := strings.Split(r.URL.Path, "/")
	len := len(path)
	name := path[len-1]
	kind := path[len-2]
	namespace := path[len-3]
	return ResourceInfo{
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
		Verb:       Patch,
		StatusCode: statusCode,
	}
}

func handleUpdate(r *http.Request, statusCode int) ResourceInfo {
	path := strings.Split(r.URL.Path, "/")
	len := len(path)
	name := path[len-1]
	kind := path[len-2]
	namespace := path[len-3]
	return ResourceInfo{
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
		Verb:       Update,
		StatusCode: statusCode,
	}
}

func (mrt *metricsRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, roundTimeErr := mrt.roundTripper.RoundTrip(r)
	var info ResourceInfo
	switch verb := mrt.resolveK8sRequestVerb(r); verb {
	case List:
		info = handleList(r, resp.StatusCode)
	case Get:
		info = handleGet(r, resp.StatusCode)
	case Create:
		info = handleCreate(r, resp.StatusCode)
	case Delete:
		info = handleDelete(r, resp.StatusCode)
	case Patch:
		info = handlePatch(r, resp.StatusCode)
	case Update:
		info = handleUpdate(r, resp.StatusCode)
	default:
		log.With("path", r.URL.Path).With("method", r.Method).Warnf("Unknown Request")
		info = ResourceInfo{
			Verb:       Unknown,
			StatusCode: resp.StatusCode,
		}
	}
	mrt.inc(info)
	return resp, roundTimeErr
}

// AddMetricsTransportWrapper adds a transport wrapper which wraps a function call around each kubernetes request
func AddMetricsTransportWrapper(config *rest.Config, incFunc func(ResourceInfo) error) *rest.Config {
	wrap := config.WrapTransport
	config.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		if wrap != nil {
			rt = wrap(rt)
		}
		return &metricsRoundTripper{
			roundTripper: rt,
			inc:          incFunc,
			commonResources: map[string]bool{
				"replicasets":       true,
				"services":          true,
				"experiments":       true,
				"rollouts":          true,
				"analysistemplates": true,
				"analysisruns":      true,
				"virutalservices":   true,
				"jobs":              true,
			},
		}
	}
	return config
}
