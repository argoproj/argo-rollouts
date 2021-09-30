package ingress_test

import (
	"context"
	"testing"

	"github.com/argoproj/argo-rollouts/utils/ingress"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestNewIngressWithAnnotations(t *testing.T) {
	annotations := make(map[string]string)
	annotations["some.annotation.key1"] = "some.annotation.value1"
	annotations["some.annotation.key2"] = "some.annotation.value2"
	getAnnotations := func() map[string]string {
		annotations := make(map[string]string)
		annotations["some.annotation.key1"] = "some.annotation.value1"
		annotations["some.annotation.key2"] = "some.annotation.value2"
		return annotations
	}
	t.Run("will instantiate an Ingress wrapped with an annotated networkingv1.Ingress", func(t *testing.T) {
		// given
		t.Parallel()

		// when
		i := ingress.NewIngressWithAnnotations(ingress.IngressModeNetworking, getAnnotations())

		// then
		assert.NotNil(t, i)
		a := i.GetAnnotations()
		assert.Equal(t, 2, len(a))
		a["extra-annotation-key"] = "extra-annotation-value"
		i.SetAnnotations(a)
		assert.Equal(t, 3, len(a))
	})
	t.Run("will instantiate an Ingress wrapped with an annotated extensions/v1beta1.Ingress", func(t *testing.T) {
		// given
		t.Parallel()

		// when
		i := ingress.NewIngressWithAnnotations(ingress.IngressModeExtensions, getAnnotations())

		// then
		assert.NotNil(t, i)
		a := i.GetAnnotations()
		assert.Equal(t, 2, len(a))
		a["extra-annotation-key"] = "extra-annotation-value"
		i.SetAnnotations(a)
		assert.Equal(t, 3, len(a))
	})
	t.Run("will return nil if ingress mode is undefined", func(t *testing.T) {
		// given
		t.Parallel()

		// when
		i := ingress.NewIngressWithAnnotations(99999, getAnnotations())

		// then
		assert.Nil(t, i)
	})
}

func TestGetExtensionsIngress(t *testing.T) {
	extensionsIngress := &v1beta1.Ingress{}
	t.Run("will get extensions ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		i := ingress.NewLegacyIngress(extensionsIngress)

		// when
		result, err := i.GetExtensionsIngress()

		// then
		assert.Nil(t, err)
		assert.NotNil(t, result)
	})
	t.Run("will return error if wrapper has nil reference to the extensionsIngress", func(t *testing.T) {
		// given
		t.Parallel()
		i := ingress.NewLegacyIngress(nil)

		// when
		result, err := i.GetExtensionsIngress()

		// then
		assert.NotNil(t, err)
		assert.Nil(t, result)
	})
}

func TestGetNetworkingIngress(t *testing.T) {
	networkingIngress := &v1.Ingress{}
	t.Run("will get networkingv1 ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		i := ingress.NewIngress(networkingIngress)

		// when
		result, err := i.GetNetworkingIngress()

		// then
		assert.Nil(t, err)
		assert.NotNil(t, result)
	})
	t.Run("will return error if wrapper has nil reference to the networkingIngress", func(t *testing.T) {
		// given
		t.Parallel()
		i := ingress.NewIngress(nil)

		// when
		result, err := i.GetNetworkingIngress()

		// then
		assert.NotNil(t, err)
		assert.Nil(t, result)
	})
}

func TestGetLabels(t *testing.T) {
	t.Run("will get the labels from network Ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		i := getNetworkingIngress()
		w := ingress.NewIngress(i)

		// when
		labels := w.GetLabels()

		// then
		assert.Equal(t, 2, len(labels))
		assert.Equal(t, "label-value1", labels["label-key1"])
		assert.Equal(t, "label-value2", labels["label-key2"])
	})
	t.Run("will get labels from extensions Ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		i := getExtensionsIngress()

		// when
		w := ingress.NewLegacyIngress(i)

		// when
		labels := w.GetLabels()

		// then
		assert.Equal(t, 2, len(labels))
		assert.Equal(t, "label-value1", labels["label-key1"])
		assert.Equal(t, "label-value2", labels["label-key2"])
	})
}

func TestGetObjectMeta(t *testing.T) {
	t.Run("will get object meta from wrapper with networking ingress", func(t *testing.T) {
		// given
		t.Parallel()
		i := getNetworkingIngress()
		ni := ingress.NewIngress(i)

		// when
		om := ni.GetObjectMeta()

		// then
		assert.Equal(t, "networking-ingress", om.GetName())
		assert.Equal(t, "some-namespace", om.GetNamespace())
		assert.Equal(t, 2, len(om.GetLabels()))
	})
	t.Run("will get object meta from wrapper with extensions ingress", func(t *testing.T) {
		// given
		t.Parallel()
		i := getExtensionsIngress()
		li := ingress.NewLegacyIngress(i)

		// when
		om := li.GetObjectMeta()

		// then
		assert.Equal(t, "extensions-ingress", om.GetName())
		assert.Equal(t, "some-namespace", om.GetNamespace())
		assert.Equal(t, 2, len(om.GetLabels()))
	})
}

func TestDeepCopy(t *testing.T) {
	t.Run("will deepcopy ingress wrapped with networking.Ingress", func(t *testing.T) {
		// given
		t.Parallel()
		ni := ingress.NewIngress(getNetworkingIngress())

		// when
		ni2 := ni.DeepCopy()

		// then
		assert.Equal(t, ni, ni2)
		assert.False(t, ni == ni2)
	})
	t.Run("will deepcopy ingress wrapped with extensions.Ingress", func(t *testing.T) {
		// given
		t.Parallel()
		li := ingress.NewLegacyIngress(getExtensionsIngress())

		// when
		ni2 := li.DeepCopy()

		// then
		assert.Equal(t, li, ni2)
		assert.False(t, li == ni2)
	})
}

func TestGetLoadBalancerStatus(t *testing.T) {
	t.Run("will get loadbalancer status from wrapped networking.Ingress", func(t *testing.T) {
		// given
		t.Parallel()
		i := getNetworkingIngress()
		ni := ingress.NewIngress(i)

		// when
		lbs := ni.GetLoadBalancerStatus()

		// then
		assert.Equal(t, i.Status.LoadBalancer, lbs)
	})
	t.Run("will get loadbalancer status from wrapped extensions.Ingress", func(t *testing.T) {
		// given
		t.Parallel()
		i := getExtensionsIngress()
		li := ingress.NewLegacyIngress(i)

		// when
		lbs := li.GetLoadBalancerStatus()

		// then
		assert.Equal(t, i.Status.LoadBalancer, lbs)
	})
}

func Test_IngressWrapNew(t *testing.T) {
	t.Run("will return error if invalid ingress mode is passed", func(t *testing.T) {
		// given
		t.Parallel()

		// when
		iw, err := ingress.NewIngressWrapper(9999, nil, nil)

		// then
		assert.Error(t, err)
		assert.Nil(t, iw)
	})
}

func Test_IngressWrapPatch(t *testing.T) {
	t.Run("will patch networking ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)

		// when
		ing, err := iw.Patch(context.Background(), "some-namespace", "networking-ingress", types.MergePatchType, []byte("{}"), metav1.PatchOptions{})

		// then
		assert.NoError(t, err)
		assert.NotNil(t, ing)
		ni, err := ing.GetNetworkingIngress()
		assert.NoError(t, err)
		assert.Equal(t, "backend", ni.Spec.DefaultBackend.Service.Name)
	})
	t.Run("will return error if fails to patch networking ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)

		// when
		ing, err := iw.Patch(context.Background(), "not_found", "not_found", types.MergePatchType, []byte("{}"), metav1.PatchOptions{})

		// then
		assert.Error(t, err)
		assert.Nil(t, ing)
	})
	t.Run("will patch extensions ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)

		// when
		ing, err := iw.Patch(context.Background(), "some-namespace", "extensions-ingress", types.MergePatchType, []byte("{}"), metav1.PatchOptions{})

		// then
		assert.NoError(t, err)
		assert.NotNil(t, ing)
		li, err := ing.GetExtensionsIngress()
		assert.NoError(t, err)
		assert.Equal(t, "some-service", li.Spec.Backend.ServiceName)
	})
	t.Run("will return error if fails to patch extensions ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)

		// when
		ing, err := iw.Patch(context.Background(), "not_found", "not_found", types.MergePatchType, []byte("{}"), metav1.PatchOptions{})

		// then
		assert.Error(t, err)
		assert.Nil(t, ing)
	})
}

func Test_IngressWrapUpdate(t *testing.T) {
	t.Run("will update networking ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)
		i := ingress.NewIngress(getNetworkingIngress())
		ctx := context.Background()

		// when
		result, err := iw.Update(ctx, "some-namespace", i)

		// then
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
	t.Run("will return error if fails to update networking ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)
		wrongIngressVersion := ingress.NewLegacyIngress(getExtensionsIngress())
		ctx := context.Background()

		// when
		result, err := iw.Update(ctx, "some-namespace", wrongIngressVersion)

		// then
		assert.Error(t, err)
		assert.Nil(t, result)
	})
	t.Run("will update extensions ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)
		i := ingress.NewLegacyIngress(getExtensionsIngress())
		ctx := context.Background()

		// when
		result, err := iw.Update(ctx, "some-namespace", i)

		// then
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
	t.Run("will return error if fails to update extensions ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)
		wrongIngressVersion := ingress.NewIngress(getNetworkingIngress())
		ctx := context.Background()

		// when
		result, err := iw.Update(ctx, "some-namespace", wrongIngressVersion)

		// then
		assert.Error(t, err)
		assert.Nil(t, result)
	})
	t.Run("will return error if wrapper has invalid IngressMode", func(t *testing.T) {
		// given
		t.Parallel()
		invalidIngressWrap := ingress.IngressWrap{}
		ctx := context.Background()

		// when
		i, err := invalidIngressWrap.Update(ctx, "some-namespace", nil)

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
}

func Test_IngressWrapGet(t *testing.T) {
	t.Run("will get network ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)
		ctx := context.Background()

		// when
		i, err := iw.Get(ctx, "some-namespace", "networking-ingress", metav1.GetOptions{})

		// then
		assert.NoError(t, err)
		assert.NotNil(t, i)
		assert.Equal(t, "networking-ingress", i.GetName())
		assert.Equal(t, "some-namespace", i.GetNamespace())
	})
	t.Run("will return error if fails to get networking ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)
		ctx := context.Background()

		// when
		i, err := iw.Get(ctx, "not_found", "not_found", metav1.GetOptions{})

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
	t.Run("will get extensions ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)
		ctx := context.Background()

		// when
		i, err := iw.Get(ctx, "some-namespace", "extensions-ingress", metav1.GetOptions{})

		// then
		assert.NoError(t, err)
		assert.NotNil(t, i)
		assert.Equal(t, "extensions-ingress", i.GetName())
		assert.Equal(t, "some-namespace", i.GetNamespace())
	})
	t.Run("will return error if fails to get extensions ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)
		ctx := context.Background()

		// when
		i, err := iw.Get(ctx, "not_found", "not_found", metav1.GetOptions{})

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
	t.Run("will return error if wrapper has invalid IngressMode", func(t *testing.T) {
		// given
		t.Parallel()
		invalidIngressWrap := ingress.IngressWrap{}
		ctx := context.Background()

		// when
		i, err := invalidIngressWrap.Get(ctx, "some-namespace", "extensions-ingress", metav1.GetOptions{})

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
}

func Test_IngressWrapGetCached(t *testing.T) {
	t.Run("will get cached network ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)

		// when
		i, err := iw.GetCached("some-namespace", "networking-ingress")

		// then
		assert.NoError(t, err)
		assert.NotNil(t, i)
		assert.Equal(t, "networking-ingress", i.GetName())
		assert.Equal(t, "some-namespace", i.GetNamespace())
	})
	t.Run("will return error if fails to get cached networking ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)

		// when
		i, err := iw.GetCached("not_found", "not_found")

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
	t.Run("will get cached extensions ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)

		// when
		i, err := iw.GetCached("some-namespace", "extensions-ingress")

		// then
		assert.NoError(t, err)
		assert.NotNil(t, i)
		assert.Equal(t, "extensions-ingress", i.GetName())
		assert.Equal(t, "some-namespace", i.GetNamespace())
	})
	t.Run("will return error if fails to get extensions ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)

		// when
		i, err := iw.GetCached("not_found", "not_found")

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
	t.Run("will return error if wrapper has invalid IngressMode", func(t *testing.T) {
		// given
		t.Parallel()
		invalidIngressWrap := ingress.IngressWrap{}

		// when
		i, err := invalidIngressWrap.GetCached("some-namespace", "extensions-ingress")

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
}

func Test_IngressWrapCreate(t *testing.T) {
	t.Run("will create network ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)
		ctx := context.Background()
		ni := getNetworkingIngress()
		ni.SetNamespace("different-namespace")
		i := ingress.NewIngress(ni)

		// when
		i, err := iw.Create(ctx, "different-namespace", i, metav1.CreateOptions{})

		// then
		assert.NoError(t, err)
		assert.NotNil(t, i)
		assert.Equal(t, "networking-ingress", i.GetName())
		assert.Equal(t, "different-namespace", i.GetNamespace())
	})
	t.Run("will return error if fails to create networking ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)
		ctx := context.Background()
		i := ingress.NewIngress(getNetworkingIngress())

		// when
		i, err := iw.Create(ctx, "some-namespace", i, metav1.CreateOptions{})

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
	t.Run("will create extensions ingress successfully", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)
		ctx := context.Background()
		li := getExtensionsIngress()
		li.SetNamespace("different-namespace")
		i := ingress.NewLegacyIngress(li)

		// when
		i, err := iw.Create(ctx, "different-namespace", i, metav1.CreateOptions{})

		// then
		assert.NoError(t, err)
		assert.NotNil(t, i)
		assert.Equal(t, "extensions-ingress", i.GetName())
		assert.Equal(t, "different-namespace", i.GetNamespace())
	})
	t.Run("will return error if fails to create extensions ingress", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)
		ctx := context.Background()
		i := ingress.NewLegacyIngress(getExtensionsIngress())

		// when
		i, err := iw.Create(ctx, "some-namespace", i, metav1.CreateOptions{})

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
	t.Run("will return error if wrapper has invalid IngressMode", func(t *testing.T) {
		// given
		t.Parallel()
		invalidIngressWrap := ingress.IngressWrap{}
		i := ingress.NewLegacyIngress(getExtensionsIngress())
		ctx := context.Background()

		// when
		i, err := invalidIngressWrap.Create(ctx, "some-namespace", i, metav1.CreateOptions{})

		// then
		assert.Error(t, err)
		assert.Nil(t, i)
	})
}

func Test_IngressWrapHasSynced(t *testing.T) {
	t.Run("will check networking ingress HasSynced", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeNetworking)

		// when
		synced := iw.HasSynced()

		// then
		assert.False(t, synced)
	})
	t.Run("will check extensions ingress HasSynced", func(t *testing.T) {
		// given
		t.Parallel()
		iw := newMockedIngressWrapper(t, ingress.IngressModeExtensions)

		// when
		synced := iw.HasSynced()

		// then
		assert.False(t, synced)
	})
	t.Run("will return false if wrapper has invalid IngressMode", func(t *testing.T) {
		// given
		t.Parallel()
		iw := ingress.IngressWrap{}

		// when
		synced := iw.HasSynced()

		// then
		assert.False(t, synced)
	})
}

func newMockedIngressWrapper(t *testing.T, mode ingress.IngressMode) *ingress.IngressWrap {
	t.Helper()
	kubeclient := k8sfake.NewSimpleClientset(getNetworkingIngress(), getExtensionsIngress())
	informer := kubeinformers.NewSharedInformerFactory(kubeclient, 0)
	informer.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(getExtensionsIngress())
	informer.Networking().V1().Ingresses().Informer().GetIndexer().Add(getNetworkingIngress())

	i, err := ingress.NewIngressWrapper(mode, kubeclient, informer)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func getNetworkingIngress() *v1.Ingress {
	ingressClassName := "ingress-name"
	return &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "networking-ingress",
			Namespace: "some-namespace",
			Labels: map[string]string{
				"label-key1": "label-value1",
				"label-key2": "label-value2",
			},
		},
		Spec: v1.IngressSpec{
			IngressClassName: &ingressClassName,
			DefaultBackend: &v1.IngressBackend{
				Service: &v1.IngressServiceBackend{
					Name: "backend",
					Port: v1.ServiceBackendPort{
						Name:   "http",
						Number: 8080,
					},
				},
			},
		},
		Status: v1.IngressStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP:       "127.0.0.1",
						Hostname: "localhost",
						Ports: []corev1.PortStatus{
							{
								Port:     8080,
								Protocol: "http",
							},
						},
					},
				},
			},
		},
	}
}

func getExtensionsIngress() *v1beta1.Ingress {
	ingressClassName := "ingress-name"
	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "extensions-ingress",
			Namespace: "some-namespace",
			Labels: map[string]string{
				"label-key1": "label-value1",
				"label-key2": "label-value2",
			},
		},
		Spec: v1beta1.IngressSpec{
			IngressClassName: &ingressClassName,
			Backend: &v1beta1.IngressBackend{
				ServiceName: "some-service",
				ServicePort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "8080",
				},
			},
		},
		Status: v1beta1.IngressStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP:       "127.0.0.1",
						Hostname: "localhost",
						Ports: []corev1.PortStatus{
							{
								Port:     8080,
								Protocol: "http",
							},
						},
					},
				},
			},
		},
	}
}
