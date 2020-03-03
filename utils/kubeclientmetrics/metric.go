package kubeclientmetrics

import (
	"io/ioutil"
	"net/http"
	"path"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
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
	roundTripper http.RoundTripper
	inc          func(ResourceInfo) error
}

// isGetOrList Uses a path from a request to determine if the request is a GET or LIST. The function tries to find an
// API version within the path and then calculates how many remaining segments are after the API version. A LIST request
// has segments for the kind with a namespace and the specific namespace if the kind is a namespaced resource.
// Meanwhile a GET request has an additional segment for resource name. As a result, a LIST has an odd number of
// segments while a GET request has an even number of segments.
func isGetOrList(r *http.Request) K8sRequestVerb {
	// The following code checks if the path ends with  value of the path is a resource name or kind.
	// finds the API version in the url and
	regex, err := regexp.Compile(`v1\w*?(/[a-zA-Z0-9-]*)(/[a-zA-Z0-9-]*)?(/[a-zA-Z0-9-]*)?(/[a-zA-Z0-9-]*)?`)
	if err != nil {
		panic(err)
	}
	segements := regex.FindStringSubmatch(r.URL.Path)
	unusedGroup := 0
	for _, str := range segements {
		if str == "" {
			unusedGroup++
		}
	}
	if unusedGroup%2 == 1 {
		return List
	}
	return Get
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
		return isGetOrList(r)
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
		log.WithField("Kind", kind).Warn("Unable to Process Create request")
		return ResourceInfo{}
	}
	body, err := ioutil.ReadAll(bodyIO)
	if err != nil {
		log.WithField("Kind", kind).Warn("Unable to Process Create request")
		return ResourceInfo{}
	}
	obj, err := unstructured.StrToUnstructured(string(body))
	if err != nil {
		log.WithField("Kind", kind).Warn("Unable to Process Create request")
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
		log.WithField("path", r.URL.Path).WithField("method", r.Method).Warnf("Unknown Request")
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
		}
	}
	return config
}
