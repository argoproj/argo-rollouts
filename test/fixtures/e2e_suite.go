package fixtures

import (
	"flag"
	"fmt"
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
	// E2E_INSTANCE_ID is the instance id label attached to objects created by the e2e tests
	EnvVarE2EInstanceID = "E2E_INSTANCE_ID"
	// E2E_WAIT_TIMEOUT is a timeout in seconds when waiting for a test condition (default: 60)
	EnvVarE2EWaitTimeout = "E2E_WAIT_TIMEOUT"
	// E2E_POD_DELAY slows down pod startup and shutdown by the value in seconds (default: 0)
	// Used humans slow down rollout activity during a test
	EnvVarE2EPodDelay = "E2E_POD_DELAY"
	// E2E_DEBUG makes e2e testing easier to debug by not tearing down the suite
	EnvVarE2EDebug = "E2E_DEBUG"
)

var (
	E2EWaitTimeout time.Duration = time.Second * 60
	E2EPodDelay                  = 0

	// All e2e tests will be labeled with this instance-id (unless E2E_INSTANCE_ID="")
	E2ELabelValueInstanceID = "argo-rollouts-e2e"
	// All e2e tests will be labeled with their test name
	E2ELabelKeyTestName = "e2e-test-name"

	serviceGVR = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "services",
	}
	ingressGVR = schema.GroupVersionResource{
		Group:    "networking.k8s.io",
		Version:  "v1",
		Resource: "ingresses",
	}
)

func init() {
	if instanceID, ok := os.LookupEnv(EnvVarE2EInstanceID); ok {
		E2ELabelValueInstanceID = instanceID
	}
	if e2eWaitTimeout, ok := os.LookupEnv(EnvVarE2EWaitTimeout); ok {
		timeout, err := strconv.Atoi(e2eWaitTimeout)
		if err != nil {
			panic(fmt.Sprintf("Invalid wait timeout seconds: %s", e2eWaitTimeout))
		}
		E2EWaitTimeout = time.Duration(timeout) * time.Second
	}
	if e2ePodDelay, ok := os.LookupEnv(EnvVarE2EPodDelay); ok {
		delay, err := strconv.Atoi(e2ePodDelay)
		if err != nil {
			panic(fmt.Sprintf("Invalid pod delay value: %s", e2ePodDelay))
		}
		E2EPodDelay = delay
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
	s.Common.kubernetesHost = restConfig.Host
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
}

func (s *E2ESuite) TearDownSuite() {
	if os.Getenv(EnvVarE2EDebug) == "true" {
		s.log.Info("skipping resource cleanup")
		return
	}
	s.DeleteResources()
}

func (s *E2ESuite) BeforeTest(suiteName, testName string) {
	s.DeleteResources()
}

func (s *E2ESuite) AfterTest(_, _ string) {
	if s.Common.t.Failed() && s.rollout != nil {
		s.PrintRollout(s.rollout)
	}
}

func (s *E2ESuite) DeleteResources() {
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
	req, err := labels.NewRequirement(E2ELabelKeyTestName, selection.Exists, []string{})
	s.CheckError(err)

	foregroundDelete := metav1.DeletePropagationForeground
	deleteOpts := &metav1.DeleteOptions{PropagationPolicy: &foregroundDelete}
	listOpts := metav1.ListOptions{LabelSelector: req.String()}

	listResources := func(gvr schema.GroupVersionResource) []unstructured.Unstructured {
		var err error
		var lst *unstructured.UnstructuredList
		if gvr == rov1.ClusterAnalysisTemplateGVR {
			lst, err = s.dynamicClient.Resource(gvr).List(listOpts)
		} else {
			lst, err = s.dynamicClient.Resource(gvr).Namespace(s.namespace).List(listOpts)
		}
		if err != nil && !k8serrors.IsNotFound(err) {
			s.CheckError(err)
		}
		if lst == nil {
			return nil
		}
		return lst.Items
	}

	// Delete all resources with test label
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
