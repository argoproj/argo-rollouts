package fixtures

import (
	"flag"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/suite"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
)

const (
	DefaultTimeout time.Duration = time.Second * 30
)

var (
	E2ELabel = "argo-rollouts-e2e"

	serviceGVR = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "services",
	}
	ingressGVR = schema.GroupVersionResource{
		Version:  "networking.k8s.io",
		Resource: "ingresses",
	}
)

func init() {
	if e2elabel, ok := os.LookupEnv("E2E_INSTANCE_ID"); ok {
		E2ELabel = e2elabel
	}
}

type E2ESuite struct {
	suite.Suite
	Common
}

func (s *E2ESuite) SetupSuite() {
	var err error
	s.Common.t = s.Suite.T()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	restConfig, err := config.ClientConfig()
	s.CheckError(err)
	restConfig.Burst = 50
	restConfig.QPS = 20
	s.namespace, _, err = config.Namespace()
	s.CheckError(err)
	s.kubeClient, err = kubernetes.NewForConfig(restConfig)
	s.CheckError(err)
	s.dynamicClient, err = dynamic.NewForConfig(restConfig)
	s.CheckError(err)
	s.rolloutClient, err = clientset.NewForConfig(restConfig)
	s.CheckError(err)
	s.log = log.NewEntry(log.StandardLogger())

	if !flag.Parsed() {
		klog.InitFlags(nil)
		_ = flag.Set("logtostderr", "true")
		_ = flag.Set("v", strconv.Itoa(7))
		flag.Parse()
	}

	if delayStr := os.Getenv("E2E_POD_DELAY"); delayStr != "" {
		delay, err := strconv.Atoi(delayStr)
		s.CheckError(err)
		s.podDelay = delay
	}
}

func (s *E2ESuite) TearDownSuite() {
	s.DeleteResources(E2ELabel)
}

func (s *E2ESuite) BeforeTest(suiteName, testName string) {
	s.DeleteResources(E2ELabel)
}

func (s *E2ESuite) AfterTest(_, _ string) {
}

func (s *E2ESuite) DeleteResources(label string) {
	resources := []schema.GroupVersionResource{
		rov1.RolloutGVR,
		rov1.AnalysisRunGVR,
		rov1.AnalysisTemplateGVR,
		rov1.ClusterAnalysisTemplateGVR,
		rov1.ExperimentGVR,
		serviceGVR,
		ingressGVR,
		istioutil.GetIstioGVR("v1alpha3"),
	}
	req, err := labels.NewRequirement(rov1.LabelKeyControllerInstanceID, selection.Equals, []string{E2ELabel})
	s.CheckError(err)

	foregroundDelete := metav1.DeletePropagationForeground
	deleteOpts := &metav1.DeleteOptions{PropagationPolicy: &foregroundDelete}
	listOpts := metav1.ListOptions{LabelSelector: req.String()}

	listResources := func(gvr schema.GroupVersionResource) []unstructured.Unstructured {
		var err error
		var lst *unstructured.UnstructuredList
		if gvr == rov1.ClusterAnalysisTemplateGVR {
			lst, err = s.dynamicClient.Resource(gvr).List(metav1.ListOptions{LabelSelector: req.String()})
		} else {
			lst, err = s.dynamicClient.Resource(gvr).Namespace(s.namespace).List(metav1.ListOptions{LabelSelector: req.String()})
		}
		if err != nil && !k8serrors.IsNotFound(err) {
			s.CheckError(err)
		}
		if lst == nil {
			return nil
		}
		return lst.Items
	}

	// Delete all resources with test instance id
	resourcesRemaining := resources[:0]
	for _, gvr := range resources {
		switch gvr {
		case rov1.ClusterAnalysisTemplateGVR:
			err = s.dynamicClient.Resource(gvr).DeleteCollection(deleteOpts, listOpts)
		case serviceGVR:
			// Services do not support deletecollection
			for _, obj := range listResources(gvr) {
				if obj.GetDeletionTimestamp() != nil {
					continue
				}
				err = s.dynamicClient.Resource(gvr).Namespace(s.namespace).Delete(obj.GetName(), deleteOpts)
				s.CheckError(err)
			}
		default:
			// NOTE: deletecollection does not appear to work without supplying a namespace.
			// It errors with: the server could not find the requested resource
			err = s.dynamicClient.Resource(gvr).Namespace(s.namespace).DeleteCollection(deleteOpts, listOpts)
		}
		if err != nil && !k8serrors.IsNotFound(err) {
			s.log.Fatalf("could not delete %v: %v", gvr, err)
		}
		count := len(listResources(gvr))
		if count > 0 {
			s.log.Infof("Waiting for %d %s to delete", count, gvr.Resource)
			resourcesRemaining = append(resourcesRemaining, gvr)
		}
	}
	resources = resourcesRemaining

	// Wait for all instances to become deleted
	for {
		resourcesRemaining := resources[:0]
		for _, gvr := range resources {
			if len(listResources(gvr)) > 0 {
				resourcesRemaining = append(resourcesRemaining, gvr)
			}
		}
		resources = resourcesRemaining
		if len(resources) == 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}
}

func (s *E2ESuite) Run(name string, subtest func()) {
	// This add demarcation to the logs making it easier to differentiate the output of different tests.
	longName := s.Common.t.Name() + "/" + name
	log.Debug("=== RUN " + longName)
	defer func() {
		if s.Common.t.Failed() {
			log.Debug("=== FAIL " + longName)
			s.Common.t.FailNow()
		} else if s.Common.t.Skipped() {
			log.Debug("=== SKIP " + longName)
		} else {
			log.Debug("=== PASS " + longName)
		}
	}()
	s.Suite.Run(name, subtest)
}

func (s *E2ESuite) Given() *Given {
	return &Given{
		Common: s.Common,
	}
}
