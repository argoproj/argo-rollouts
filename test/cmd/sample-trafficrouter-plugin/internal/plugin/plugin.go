package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/sirupsen/logrus"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var _ rpc.TrafficRouterPlugin = &RpcPlugin{}

type RpcPlugin struct {
	LogCtx         *logrus.Entry
	ingressWrapper *ingressutil.IngressWrap
}

func (p *RpcPlugin) InitPlugin() pluginTypes.RpcError {
	p.LogCtx.Info("InitPlugin")

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	// if you want to change the loading rules (which files in which order), you can do so here
	configOverrides := &clientcmd.ConfigOverrides{}
	// if you want to change override values or bind them to flags, there are methods to help you
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	kubeClient, _ := kubernetes.NewForConfig(config)

	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		5*time.Minute,
		kubeinformers.WithNamespace(metav1.NamespaceAll))

	mode, _ := ingressutil.DetermineIngressMode("", kubeClient.DiscoveryClient)
	ingressWrapper, _ := ingressutil.NewIngressWrapper(mode, kubeClient, kubeInformerFactory)
	p.ingressWrapper = ingressWrapper
	go p.ingressWrapper.Informer().Run(context.Background().Done())
	cache.WaitForCacheSync(context.Background().Done(), p.ingressWrapper.Informer().HasSynced)
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) buildCanaryIngress(ro *v1alpha1.Rollout, stableIngress *networkingv1.Ingress, name string, desiredWeight int32) (*ingressutil.Ingress, error) {
	stableIngressName := "canary-demo"
	stableServiceName := ro.Spec.Strategy.Canary.StableService
	canaryServiceName := ro.Spec.Strategy.Canary.CanaryService
	annotationPrefix := defaults.GetCanaryIngressAnnotationPrefixOrDefault(ro)

	// Set up canary ingress resource, we do *not* have to duplicate `spec.tls` in a canary, only
	// `spec.rules`
	desiredCanaryIngress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Spec: networkingv1.IngressSpec{
			Rules: make([]networkingv1.IngressRule, 0), // We have no way of knowing yet how many rules there will be
		},
	}

	// Preserve ingressClassName from stable ingress
	if stableIngress.Spec.IngressClassName != nil {
		desiredCanaryIngress.Spec.IngressClassName = stableIngress.Spec.IngressClassName
	}

	// Must preserve ingress.class on canary ingress, no other annotations matter
	// See: https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary
	if val, ok := stableIngress.Annotations["kubernetes.io/ingress.class"]; ok {
		desiredCanaryIngress.Annotations["kubernetes.io/ingress.class"] = val
	}

	// Ensure canaryIngress is owned by this Rollout for cleanup
	desiredCanaryIngress.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(ro, v1alpha1.SchemeGroupVersion.WithKind("Rollout"))})

	// Copy only the rules which reference the stableService from the stableIngress to the canaryIngress
	// and change service backend to canaryService. Rules **not** referencing the stableIngress will be ignored.
	for ir := 0; ir < len(stableIngress.Spec.Rules); ir++ {
		var hasStableServiceBackendRule bool
		ingressRule := stableIngress.Spec.Rules[ir].DeepCopy()

		// Update all backends pointing to the stableService to point to the canaryService now
		for ip := 0; ip < len(ingressRule.HTTP.Paths); ip++ {
			if ingressRule.HTTP.Paths[ip].Backend.Service.Name == stableServiceName {
				hasStableServiceBackendRule = true
				ingressRule.HTTP.Paths[ip].Backend.Service.Name = canaryServiceName
			}
		}

		// If this rule was using the specified stableService backend, append it to the canary Ingress spec
		if hasStableServiceBackendRule {
			desiredCanaryIngress.Spec.Rules = append(desiredCanaryIngress.Spec.Rules, *ingressRule)
		}
	}

	if len(desiredCanaryIngress.Spec.Rules) == 0 {
		return nil, fmt.Errorf("ingress `%s` has no rules using service %s backend", stableIngressName, stableServiceName)
	}

	// Process additional annotations, would commonly be things like `canary-by-header` or `load-balance`
	//for k, v := range ro.Spec.Strategy.Canary.TrafficRouting.Nginx.AdditionalIngressAnnotations {
	//	if !strings.HasPrefix(k, annotationPrefix) {
	//		k = fmt.Sprintf("%s/%s", annotationPrefix, k)
	//	}
	//	desiredCanaryIngress.Annotations[k] = v
	//}
	// Always set `canary` and `canary-weight` - `canary-by-header` and `canary-by-cookie`, if set,  will always take precedence
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary", annotationPrefix)] = "true"
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary-weight", annotationPrefix)] = fmt.Sprintf("%d", desiredWeight)

	return ingressutil.NewIngress(desiredCanaryIngress), nil
}

func (r *RpcPlugin) buildLegacyCanaryIngress(ro *v1alpha1.Rollout, stableIngress *extensionsv1beta1.Ingress, name string, desiredWeight int32) (*ingressutil.Ingress, error) {
	stableIngressName := "canary-demo"
	stableServiceName := ro.Spec.Strategy.Canary.StableService
	canaryServiceName := ro.Spec.Strategy.Canary.CanaryService
	annotationPrefix := defaults.GetCanaryIngressAnnotationPrefixOrDefault(ro)

	// Set up canary ingress resource, we do *not* have to duplicate `spec.tls` in a canary, only
	// `spec.rules`
	desiredCanaryIngress := &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: make([]extensionsv1beta1.IngressRule, 0), // We have no way of knowing yet how many rules there will be
		},
	}

	// Preserve ingressClassName from stable ingress
	if stableIngress.Spec.IngressClassName != nil {
		desiredCanaryIngress.Spec.IngressClassName = stableIngress.Spec.IngressClassName
	}

	// Must preserve ingress.class on canary ingress, no other annotations matter
	// See: https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary
	if val, ok := stableIngress.Annotations["kubernetes.io/ingress.class"]; ok {
		desiredCanaryIngress.Annotations["kubernetes.io/ingress.class"] = val
	}

	// Ensure canaryIngress is owned by this Rollout for cleanup
	desiredCanaryIngress.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(ro, v1alpha1.SchemeGroupVersion.WithKind("Rollout"))})

	// Copy only the rules which reference the stableService from the stableIngress to the canaryIngress
	// and change service backend to canaryService. Rules **not** referencing the stableIngress will be ignored.
	for ir := 0; ir < len(stableIngress.Spec.Rules); ir++ {
		var hasStableServiceBackendRule bool
		ingressRule := stableIngress.Spec.Rules[ir].DeepCopy()

		// Update all backends pointing to the stableService to point to the canaryService now
		for ip := 0; ip < len(ingressRule.HTTP.Paths); ip++ {
			if ingressRule.HTTP.Paths[ip].Backend.ServiceName == stableServiceName {
				hasStableServiceBackendRule = true
				ingressRule.HTTP.Paths[ip].Backend.ServiceName = canaryServiceName
			}
		}

		// If this rule was using the specified stableService backend, append it to the canary Ingress spec
		if hasStableServiceBackendRule {
			desiredCanaryIngress.Spec.Rules = append(desiredCanaryIngress.Spec.Rules, *ingressRule)
		}
	}

	if len(desiredCanaryIngress.Spec.Rules) == 0 {
		return nil, fmt.Errorf("ingress `%s` has no rules using service %s backend", stableIngressName, stableServiceName)
	}

	// Process additional annotations, would commonly be things like `canary-by-header` or `load-balance`
	for k, v := range ro.Spec.Strategy.Canary.TrafficRouting.Nginx.AdditionalIngressAnnotations {
		if !strings.HasPrefix(k, annotationPrefix) {
			k = fmt.Sprintf("%s/%s", annotationPrefix, k)
		}
		desiredCanaryIngress.Annotations[k] = v
	}
	// Always set `canary` and `canary-weight` - `canary-by-header` and `canary-by-cookie`, if set,  will always take precedence
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary", annotationPrefix)] = "true"
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary-weight", annotationPrefix)] = fmt.Sprintf("%d", desiredWeight)

	return ingressutil.NewLegacyIngress(desiredCanaryIngress), nil
}

// canaryIngress returns the desired state of the canary ingress
func (r *RpcPlugin) canaryIngress(ro *v1alpha1.Rollout, stableIngress *ingressutil.Ingress, name string, desiredWeight int32) (*ingressutil.Ingress, error) {
	switch stableIngress.Mode() {
	case ingressutil.IngressModeNetworking:
		networkingIngress, err := stableIngress.GetNetworkingIngress()
		if err != nil {
			return nil, err
		}
		return r.buildCanaryIngress(ro, networkingIngress, name, desiredWeight)
	case ingressutil.IngressModeExtensions:
		extensionsIngress, err := stableIngress.GetExtensionsIngress()
		if err != nil {
			return nil, err
		}
		return r.buildLegacyCanaryIngress(ro, extensionsIngress, name, desiredWeight)
	default:
		return nil, errors.New("undefined ingress mode")
	}
}

// SetWeight modifies Nginx Ingress resources to reach desired state
func (r *RpcPlugin) SetWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	ctx := context.TODO()

	s := v1alpha1.NginxTrafficRouting{}
	err := json.Unmarshal(ro.Spec.Strategy.Canary.TrafficRouting.Plugins["argoproj/sample-nginx"], &s)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: "could not unmarshal nginx config"}
	}

	stableIngressName := s.StableIngress
	canaryIngressName := getCanaryIngressName(ro, stableIngressName)

	// Check if stable ingress exists (from lister, which has a cache), error if it does not
	stableIngress, err := r.ingressWrapper.GetCached(ro.Namespace, stableIngressName)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("error retrieving stableIngress `%s` from cache: %v", stableIngressName, err)}
	}
	// Check if canary ingress exists (from lister which has a cache), determines whether we later call Create() or Update()
	canaryIngress, err := r.ingressWrapper.GetCached(ro.Namespace, canaryIngressName)

	canaryIngressExists := true
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			// An error other than "not found" occurred
			return pluginTypes.RpcError{ErrorString: fmt.Sprintf("error retrieving canary ingress `%s` from cache: %v", canaryIngressName, err)}
		}
		canaryIngressExists = false
	}

	// Construct the desired canary Ingress resource
	desiredCanaryIngress, err := r.canaryIngress(ro, stableIngress, canaryIngressName, desiredWeight)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	if !canaryIngressExists {
		_, err = r.ingressWrapper.Create(ctx, ro.Namespace, desiredCanaryIngress, metav1.CreateOptions{})
		if err == nil {
			return pluginTypes.RpcError{}
		}
		if !k8serrors.IsAlreadyExists(err) {
			//r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error creating canary ingress")
			return pluginTypes.RpcError{ErrorString: fmt.Sprintf("error creating canary ingress `%s`: %v", canaryIngressName, err)}
		}
		// Canary ingress was created by a different reconcile call before this one could complete (race)
		// This means we just read it from the API now (instead of cache) and continue with the normal
		// flow we take when the canary already existed.
		canaryIngress, err = r.ingressWrapper.Get(ctx, ro.Namespace, canaryIngressName, metav1.GetOptions{})
		if err != nil {
			//r.log.WithField(logutil.IngressKey, canaryIngressName).Error(err.Error())
			return pluginTypes.RpcError{ErrorString: fmt.Sprintf("error retrieving canary ingress `%s` from api: %v", canaryIngressName, err)}
		}
	}

	// Canary Ingress already exists, apply a patch if needed

	// Only modify canaryIngress if it is controlled by this Rollout
	if !metav1.IsControlledBy(canaryIngress.GetObjectMeta(), ro) {
		//r.log.WithField(logutil.IngressKey, canaryIngressName).Error("canary ingress controlled by different object")
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("canary ingress `%s` controlled by different object", canaryIngressName)}
	}

	// Make patches
	patch, modified, err := ingressutil.BuildIngressPatch(canaryIngress.Mode(), canaryIngress,
		desiredCanaryIngress, ingressutil.WithAnnotations(), ingressutil.WithLabels(), ingressutil.WithSpec())

	if err != nil {
		//r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error constructing canary ingress patch")
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("error constructing canary ingress patch for `%s`: %v", canaryIngressName, err)}
	}
	if !modified {
		//r.log.WithField(logutil.IngressKey, canaryIngressName).Info("No changes to canary ingress - skipping patch")
		return pluginTypes.RpcError{}
	}

	//r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("patch", string(patch)).Debug("applying canary Ingress patch")
	//r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("desiredWeight", desiredWeight).Info("updating canary Ingress")
	//r.cfg.Recorder.Eventf(r.cfg.Rollout, record.EventOptions{EventReason: "PatchingCanaryIngress"}, "Updating Ingress `%s` to desiredWeight '%d'", canaryIngressName, desiredWeight)

	_, err = r.ingressWrapper.Patch(ctx, ro.Namespace, canaryIngressName, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		//r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error patching canary ingress")
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("error patching canary ingress `%s`: %v", canaryIngressName, err)}
	}

	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetHeaderRoute(ro *v1alpha1.Rollout, headerRouting *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) VerifyWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (pluginTypes.RpcVerified, pluginTypes.RpcError) {
	return pluginTypes.NotImplemented, pluginTypes.RpcError{}
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *RpcPlugin) UpdateHash(ro *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetMirrorRoute(ro *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) RemoveManagedRoutes(ro *v1alpha1.Rollout) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) Type() string {
	return "plugin-nginx"
}

func getCanaryIngressName(rollout *v1alpha1.Rollout, stableIngress string) string {
	// names limited to 253 characters
	if rollout.Spec.Strategy.Canary != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Plugins != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Plugins["argoproj/sample-nginx"] != nil {

		prefix := fmt.Sprintf("%s-%s", rollout.GetName(), stableIngress)
		if len(prefix) > 253-len("-canary") {
			// trim prefix
			prefix = prefix[0 : 253-len("-canary")]
		}
		return fmt.Sprintf("%s%s", prefix, "-canary")
	}
	return ""
}
