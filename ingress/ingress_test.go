package ingress

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/utils/queue"

	"github.com/stretchr/testify/assert"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"

	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
)

const stableService string = "test-stable-service"
const additionalIngress string = "test-stable-ingress-additional"
const stableIngress string = "test-stable-ingress"

func testString(val string) string {
	return fmt.Sprintf("default/%s", val)

}

func newNginxIngress(name string, port int, serviceName string) *extensionsv1beta1.Ingress {
	class := "nginx"
	return &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: extensionsv1beta1.IngressSpec{
			IngressClassName: &class,
			Rules: []extensionsv1beta1.IngressRule{
				{
					Host: "fakehost.example.com",
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: serviceName,
										ServicePort: intstr.FromInt(port),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func newNginxIngressWithAnnotation(name string, port int, serviceName string) *extensionsv1beta1.Ingress {
	return &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "nginx",
			},
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{
				{
					Host: "fakehost.example.com",
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: serviceName,
										ServicePort: intstr.FromInt(port),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func newFakeIngressControllerMultiIngress(t *testing.T, ing []*extensionsv1beta1.Ingress, rollout *v1alpha1.Rollout) (*Controller, *k8sfake.Clientset, map[string]int) {
	return underlyingControllerBuilder(t, ing, rollout)
}

func newFakeIngressController(t *testing.T, ing *extensionsv1beta1.Ingress, rollout *v1alpha1.Rollout) (*Controller, *k8sfake.Clientset, map[string]int) {
	return underlyingControllerBuilder(t, []*extensionsv1beta1.Ingress{ing}, rollout)
}

func underlyingControllerBuilder(t *testing.T, ing []*extensionsv1beta1.Ingress, rollout *v1alpha1.Rollout) (*Controller, *k8sfake.Clientset, map[string]int) {
	t.Helper()
	client := fake.NewSimpleClientset()
	if rollout != nil {
		client = fake.NewSimpleClientset(rollout)
	}
	kubeclient := k8sfake.NewSimpleClientset()
	if ing != nil {
		var x []runtime.Object
		for _, i := range ing {
			if i != nil {
				x = append(x, i)
			}
		}
		kubeclient = k8sfake.NewSimpleClientset(x...)
	}
	i := informers.NewSharedInformerFactory(client, 0)
	k8sI := kubeinformers.NewSharedInformerFactory(kubeclient, 0)
	ingressWrap, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, kubeclient, k8sI)
	if err != nil {
		t.Fatal(err)
	}

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	ingressWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Ingresses")

	c := NewController(ControllerConfig{
		Client:           kubeclient,
		IngressWrap:      ingressWrap,
		IngressWorkQueue: ingressWorkqueue,

		RolloutsInformer: i.Argoproj().V1alpha1().Rollouts(),
		RolloutWorkQueue: rolloutWorkqueue,
		ALBClasses:       []string{"alb"},
		NGINXClasses:     []string{"nginx"},
		MetricsServer: metrics.NewMetricsServer(metrics.ServerConfig{
			Addr:               "localhost:8080",
			K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
		}),
	})
	enqueuedObjects := map[string]int{}
	var enqueuedObjectsLock sync.Mutex
	c.enqueueRollout = func(obj any) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
			panic(err)
		}
		enqueuedObjectsLock.Lock()
		defer enqueuedObjectsLock.Unlock()
		count, ok := enqueuedObjects[key]
		if !ok {
			count = 0
		}
		count++
		enqueuedObjects[key] = count
	}

	if ing != nil {
		for _, i := range ing {
			if i != nil {
				k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
			}
		}
	}
	if rollout != nil {
		i.Argoproj().V1alpha1().Rollouts().Informer().GetIndexer().Add(rollout)
	}
	return c, kubeclient, enqueuedObjects
}

func TestSyncMissingIngress(t *testing.T) {
	ctrl, _, _ := newFakeIngressController(t, nil, nil)

	err := ctrl.syncIngress(context.Background(), "default/test-ingress")
	assert.NoError(t, err)
}

func TestSyncIngressNotReferencedByRollout(t *testing.T) {

	tests := []struct {
		ings []*extensionsv1beta1.Ingress
		name string
		keys []string
	}{
		{
			[]*extensionsv1beta1.Ingress{
				newNginxIngress(stableIngress, 80, stableService),
			},
			"Single Ingress",
			[]string{
				testString(stableIngress),
			},
		},
		{
			[]*extensionsv1beta1.Ingress{
				newNginxIngress(stableIngress, 80, stableService),
				newNginxIngress(additionalIngress, 80, stableService),
			},
			"Multi Ingress",
			[]string{
				testString(stableIngress),
				testString(additionalIngress),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			ctrl, kubeclient, _ := newFakeIngressControllerMultiIngress(t, test.ings, nil)
			for _, key := range test.keys {
				err := ctrl.syncIngress(context.Background(), key)
				assert.NoError(t, err)
				actions := kubeclient.Actions()
				assert.Len(t, actions, 0)
			}
		})
	}
}

func TestSyncIngressReferencedByRollout(t *testing.T) {
	tests := []struct {
		ings                   []*extensionsv1beta1.Ingress
		name                   string
		keys                   []string
		additionalIngressNames []string
	}{
		{
			[]*extensionsv1beta1.Ingress{
				newNginxIngress(stableIngress, 80, stableService),
			},
			"Single Ingress",
			[]string{
				testString(stableIngress),
			},
			[]string{},
		},
		{
			[]*extensionsv1beta1.Ingress{
				newNginxIngress(stableIngress, 80, stableService),
				newNginxIngress(additionalIngress, 80, stableService),
			},
			"Multi Ingress",
			[]string{
				testString(stableIngress),
				testString(additionalIngress),
				testString(additionalIngress),
			},
			[]string{additionalIngress},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			rollout := &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rollout",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: v1alpha1.RolloutSpec{
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							StableService: stableService,
							CanaryService: "canary-service",
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Nginx: &v1alpha1.NginxTrafficRouting{
									StableIngress:   stableIngress,
									StableIngresses: test.additionalIngressNames,
								},
							},
						},
					},
				},
			}

			ctrl, kubeclient, enqueuedObjects := newFakeIngressControllerMultiIngress(t, test.ings, rollout)

			for i, key := range test.keys {
				err := ctrl.syncIngress(context.Background(), key)
				assert.NoError(t, err)
				actions := kubeclient.Actions()
				assert.Len(t, actions, 0)
				assert.Equal(t, i+1, enqueuedObjects["default/rollout"])
			}
		})
	}
}

func TestSkipIngressWithNoClass(t *testing.T) {
	tests := []struct {
		ings                   []*extensionsv1beta1.Ingress
		name                   string
		keys                   []string
		additionalIngressNames []string
	}{
		{
			[]*extensionsv1beta1.Ingress{
				newNginxIngressWithAnnotation(stableIngress, 80, stableService),
			},
			"Single Ingress",
			[]string{
				testString(stableIngress),
			},
			[]string{},
		},
		{
			[]*extensionsv1beta1.Ingress{
				newNginxIngressWithAnnotation(stableIngress, 80, stableService),
				newNginxIngressWithAnnotation(additionalIngress, 80, stableService),
			},
			"Multi Ingress",
			[]string{
				testString(stableIngress),
				testString(additionalIngress),
			},
			[]string{stableIngress},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, i := range test.ings {
				i.Annotations = nil
			}

			rollout := &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rollout",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: v1alpha1.RolloutSpec{
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							StableService: stableService,
							CanaryService: "canary-service",
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Nginx: &v1alpha1.NginxTrafficRouting{
									StableIngress:   stableIngress,
									StableIngresses: test.additionalIngressNames,
								},
							},
						},
					},
				},
			}

			ctrl, kubeclient, enqueuedObjects := newFakeIngressControllerMultiIngress(t, test.ings, rollout)

			for _, key := range test.keys {
				err := ctrl.syncIngress(context.Background(), key)
				assert.NoError(t, err)
				actions := kubeclient.Actions()
				assert.Len(t, actions, 0)
				assert.Len(t, enqueuedObjects, 0)
			}
		})
	}
}

func TestRun(t *testing.T) {
	// make sure we can start and top the controller
	c, _, _ := newFakeIngressController(t, nil, nil)
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	go func() {
		time.Sleep(1000 * time.Millisecond)
		c.ingressWorkqueue.ShutDownWithDrain()
		cancel()
	}()
	c.Run(ctx, 1)
}

func TestHasClass(t *testing.T) {
	tests := []struct {
		name     string
		classes  []string
		class    string
		expected bool
	}{
		// Exact match tests
		{
			name:     "exact match with single class",
			classes:  []string{"nginx"},
			class:    "nginx",
			expected: true,
		},
		{
			name:     "exact match in multiple classes",
			classes:  []string{"alb", "nginx", "traefik"},
			class:    "nginx",
			expected: true,
		},
		{
			name:     "no match",
			classes:  []string{"alb", "traefik"},
			class:    "nginx",
			expected: false,
		},
		// Wildcard match tests
		{
			name:     "wildcard matches any class",
			classes:  []string{"*"},
			class:    "nginx",
			expected: true,
		},
		{
			name:     "wildcard matches empty string",
			classes:  []string{"*"},
			class:    "",
			expected: true,
		},
		{
			name:     "wildcard in list matches",
			classes:  []string{"alb", "*", "traefik"},
			class:    "anything",
			expected: true,
		},
		// Prefix wildcard tests (*-something)
		{
			name:     "prefix wildcard matches suffix",
			classes:  []string{"*-nginx"},
			class:    "custom-nginx",
			expected: true,
		},
		{
			name:     "prefix wildcard does not match without suffix separator",
			classes:  []string{"*-nginx"},
			class:    "nginx",
			expected: false,
		},
		{
			name:     "prefix wildcard matches multiple dashes",
			classes:  []string{"*-nginx"},
			class:    "my-custom-nginx",
			expected: true,
		},
		{
			name:     "prefix wildcard no match",
			classes:  []string{"*-nginx"},
			class:    "nginx-controller",
			expected: false,
		},
		{
			name:     "prefix wildcard empty prefix matches",
			classes:  []string{"*-nginx"},
			class:    "-nginx",
			expected: true,
		},
		// Suffix wildcard tests (something-*)
		{
			name:     "suffix wildcard matches prefix",
			classes:  []string{"nginx-*"},
			class:    "nginx-custom",
			expected: true,
		},
		{
			name:     "suffix wildcard matches exact prefix",
			classes:  []string{"nginx-*"},
			class:    "nginx-",
			expected: true,
		},
		{
			name:     "suffix wildcard matches multiple dashes",
			classes:  []string{"nginx-*"},
			class:    "nginx-my-custom",
			expected: true,
		},
		{
			name:     "suffix wildcard no match",
			classes:  []string{"nginx-*"},
			class:    "controller-nginx",
			expected: false,
		},
		{
			name:     "suffix wildcard empty suffix matches",
			classes:  []string{"nginx-*"},
			class:    "nginx-",
			expected: true,
		},
		// Mixed patterns
		{
			name:     "mixed patterns first matches",
			classes:  []string{"nginx-*", "alb", "*-traefik"},
			class:    "nginx-custom",
			expected: true,
		},
		{
			name:     "mixed patterns second matches",
			classes:  []string{"nginx-*", "alb", "*-traefik"},
			class:    "alb",
			expected: true,
		},
		{
			name:     "mixed patterns third matches",
			classes:  []string{"nginx-*", "alb", "*-traefik"},
			class:    "custom-traefik",
			expected: true,
		},
		{
			name:     "mixed patterns no match",
			classes:  []string{"nginx-*", "alb", "*-traefik"},
			class:    "istio",
			expected: false,
		},
		// Edge cases
		{
			name:     "empty classes list",
			classes:  []string{},
			class:    "nginx",
			expected: false,
		},
		{
			name:     "empty class string with exact pattern",
			classes:  []string{"nginx"},
			class:    "",
			expected: false,
		},
		{
			name:     "prefix wildcard with empty pattern",
			classes:  []string{"*"},
			class:    "nginx",
			expected: true,
		},
		{
			name:     "just asterisk suffix",
			classes:  []string{"*"},
			class:    "anything-here",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasClass(tt.classes, tt.class)
			assert.Equal(t, tt.expected, result)
		})
	}
}
