package rollout

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	disco "k8s.io/client-go/discovery"
	discofake "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func newFakeDiscoClient() *discofake.FakeDiscovery {
	return &discofake.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{
					GroupVersion: corev1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{Name: "podtemplates", Namespaced: true, Kind: "PodTemplate"},
					},
				},
				{
					GroupVersion: appsv1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{Name: "deployments", Namespaced: true, Kind: "Deployment"},
						{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
					},
				},
			},
		},
	}
}

func newResolver(dynamicClient dynamic.Interface, discoveryClient disco.DiscoveryInterface, rolloutClient versioned.Interface) (*informerBasedTemplateResolver, context.CancelFunc) {
	rolloutsInformer := rolloutinformers.NewRolloutInformer(rolloutClient, "", time.Minute, cache.Indexers{})
	resolver := NewInformerBasedWorkloadRefResolver("", dynamicClient, discoveryClient, workqueue.NewDelayingQueue(), rolloutsInformer)
	stop := make(chan struct{})
	go rolloutsInformer.Run(stop)
	cache.WaitForCacheSync(stop, rolloutsInformer.HasSynced)
	return resolver, func() {
		stop <- struct{}{}
		resolver.Stop()
	}
}

func mustToUnstructured(obj runtime.Object) *unstructured.Unstructured {
	res, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: res}
}

func TestResolve_NotSupportedGroup(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-rs",
				Kind:       "argoproj",
				APIVersion: "Workflow/v1",
			},
		},
	}

	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)

	assert.Error(t, err)
}

func TestResolve_NotSupportedKind(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-rs",
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
		},
	}

	discoveryClient := &discofake.FakeDiscovery{Fake: &k8stesting.Fake{
		Resources: []*metav1.APIResourceList{
			{
				GroupVersion: appsv1.SchemeGroupVersion.String(),
				APIResources: []metav1.APIResource{
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
				},
			},
		},
	}}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)

	assert.Error(t, err)
	assert.True(t, errors.IsNotFound(err))
}

func TestResolve_UnknownAPIResource(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-deployment",
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
		},
	}

	discoveryClient := &discofake.FakeDiscovery{Fake: &k8stesting.Fake{}}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)

	assert.Error(t, err)
	assert.Equal(t, `GroupVersion "apps/v1" not found`, err.Error())
}

func TestResolve_RefDoesNotExists(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-deployment",
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
		},
	}

	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)

	assert.Error(t, err)
	assert.True(t, errors.IsNotFound(err))
}

func TestResolve_DeploymentRef(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-deployment",
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
		},
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "my-app"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test-label": "test-label-val"}},
			},
		},
	}

	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, deployment)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)

	assert.NoError(t, err)
	assert.Equal(t, deployment.Spec.Template, rollout.Spec.Template)
	assert.Equal(t, deployment.Spec.Selector, rollout.Spec.Selector)
}

func TestResolve_ReplicaSetRef(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-rs",
				Kind:       "ReplicaSet",
				APIVersion: "apps/v1",
			},
		},
	}

	rs := &appsv1.ReplicaSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "ReplicaSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-rs",
			Namespace: "default",
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test-label": "test-label-val"}},
			},
		},
	}

	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, rs)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)

	assert.NoError(t, err)
	assert.Equal(t, rs.Spec.Template, rollout.Spec.Template)
}

func TestResolveRefDeployment_PodTemplate(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-pod-template",
				Kind:       "PodTemplate",
				APIVersion: "v1",
			},
		},
	}

	rs := &corev1.PodTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "PodTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod-template",
			Namespace: "default",
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test-label": "test-label-val"}},
		},
	}

	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, rs)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)

	assert.NoError(t, err)
	assert.Equal(t, rs.Template, rollout.Spec.Template)
}

func TestRequeueReferencedRollouts(t *testing.T) {
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test-label": "test-label-val"}},
			},
		},
	}

	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Name:      "my-rollout",
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-deployment",
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
		},
	}
	rolloutsClient := fake.NewSimpleClientset(&rollout)

	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, deployment)
	resolver, cancel := newResolver(dynamicClient, discoveryClient, rolloutsClient)
	defer cancel()

	err := resolver.Resolve(&rollout)
	require.NoError(t, err)

	deploymentsClient := dynamicClient.Resource(appsv1.SchemeGroupVersion.WithResource("deployments")).Namespace("default")

	_, err = deploymentsClient.Update(context.TODO(), mustToUnstructured(deployment), v1.UpdateOptions{})
	require.NoError(t, err)

	go func() {
		// shutdown queue to make sure test fails if requeue functionality is broken
		time.Sleep(5 * time.Second)
		resolver.rolloutWorkQueue.ShutDown()
	}()

	item, done := resolver.rolloutWorkQueue.Get()
	require.False(t, done)
	assert.Equal(t, "default/my-rollout", item)
	resolver.rolloutWorkQueue.Done(item)

	err = deploymentsClient.Delete(context.TODO(), deployment.Name, v1.DeleteOptions{})
	require.NoError(t, err)

	item, done = resolver.rolloutWorkQueue.Get()
	require.False(t, done)
	assert.Equal(t, "default/my-rollout", item)
	resolver.rolloutWorkQueue.Done(item)
}

func TestRequeueReferencedRollouts_InvalidMeta(t *testing.T) {
	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	resolver.requeueReferencedRollouts(nil, schema.GroupVersionKind{})

	assert.Equal(t, 0, resolver.rolloutWorkQueue.Len())
}

func TestResolveNotRef(t *testing.T) {
	rollout := v1alpha1.Rollout{}

	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)
	assert.NoError(t, err)
	assert.Equal(t, corev1.PodTemplateSpec{}, rollout.Spec.Template)
	assert.Nil(t, rollout.Spec.Selector)
}

func TestRemashalMapFails(t *testing.T) {
	err := remarshalMap(nil, struct{}{})
	assert.Error(t, err)
}

func TestResolve_WorkloadWithTemplate(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-deployment",
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "deploy",
					},
				},
			},
		},
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deployment",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test-label": "test-label-val"}},
			},
		},
	}

	discoveryClient := newFakeDiscoClient()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, deployment)

	resolver, cancel := newResolver(dynamicClient, discoveryClient, fake.NewSimpleClientset())
	defer cancel()

	err := resolver.Resolve(&rollout)

	assert.Error(t, err)
	assert.Equal(t, "template must be empty for workload reference rollout", err.Error())
}
