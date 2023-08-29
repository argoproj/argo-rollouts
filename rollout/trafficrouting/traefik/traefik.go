package traefik

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
)

// Type holds this controller type
const (
	Type = "Traefik"

	TraefikServices                = "traefikservices"
	IngressRoutes                  = "ingressroutes"
	MirrorIngressRouteName         = "argo-mirror-ingress-route"
	DefaultMirrorIngressRouteMatch = "PathPrefix(`/`)"
	MinimumIngressRoutePriority    = 20

	TraefikServiceUpdateError = "TraefikServiceUpdateError"
	TraefikServiceCreateError = "TraefikServiceCreateError"
	TraefikServiceDeleteError = "TraefikServiceDeleteError"
	IngressRouteCreateError   = "IngressRouteCreateError"
	IngressRouteUpdateError   = "IngressRouteUpdateError"
	IngressRouteDeleteError   = "IngressRouteDeleteError"
)

var (
	MirrorIngressRoute = fmt.Sprintf(`
apiVersion: traefik.containo.us/v1alpha1
kind: IngressRoute
metadata:
  name: %s
spec:
  routes: []
`, MirrorIngressRouteName)
	MirrorTraefikService = `
apiVersion: traefik.containo.us/v1alpha1
kind: TraefikService
metadata: {}
spec:
  mirroring: {}
`
)

type ReconcilerConfig struct {
	Rollout   *v1alpha1.Rollout
	ClientSet ClientSet
	Recorder  record.EventRecorder
}

type Reconciler struct {
	Rollout   *v1alpha1.Rollout
	ClientSet ClientSet
	Recorder  record.EventRecorder
}

type ClientSet struct {
	TraefikServiceClient ClientInterface
	IngressRouteClient   ClientInterface
	ServiceClient        ServiceClientInterface
}

func (r *Reconciler) sendWarningEvent(id, msg string) {
	r.sendEvent(corev1.EventTypeWarning, id, msg)
}

func (r *Reconciler) sendEvent(eventType, id, msg string) {
	r.Recorder.Eventf(r.Rollout, record.EventOptions{EventType: eventType, EventReason: id}, msg)
}

type ClientInterface interface {
	Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error)
	Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
	Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error
}

type ServiceClientInterface interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Service, error)
}

func NewReconciler(cfg *ReconcilerConfig) *Reconciler {
	reconciler := &Reconciler{
		Rollout:   cfg.Rollout,
		ClientSet: cfg.ClientSet,
		Recorder:  cfg.Recorder,
	}
	return reconciler
}

func NewDynamicClient(di dynamic.Interface, resourceName, namespace string) dynamic.ResourceInterface {
	return di.Resource(GetMappingGVR(resourceName)).Namespace(namespace)
}

func GetMappingGVR(resourceName string) schema.GroupVersionResource {
	group := defaults.DefaultTraefikAPIGroup
	parts := strings.Split(defaults.DefaultTraefikVersion, "/")
	version := parts[len(parts)-1]
	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resourceName,
	}
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	ctx := context.TODO()
	rollout := r.Rollout
	traefikServiceName := rollout.Spec.Strategy.Canary.TrafficRouting.Traefik.WeightedTraefikServiceName
	traefikServiceClient := r.ClientSet.TraefikServiceClient
	traefikService, err := traefikServiceClient.Get(ctx, traefikServiceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	canaryServiceName := rollout.Spec.Strategy.Canary.CanaryService
	stableServiceName := rollout.Spec.Strategy.Canary.StableService
	services, isFound, err := unstructured.NestedSlice(traefikService.Object, "spec", "weighted", "services")
	if err != nil {
		return err
	}
	if !isFound {
		return errors.New("spec.weighted.services was not found in traefik service manifest")
	}
	canaryService, err := getService(canaryServiceName, services)
	if err != nil {
		return err
	}
	if canaryService == nil {
		return errors.New("traefik canary service was not found")
	}
	err = unstructured.SetNestedField(canaryService, int64(desiredWeight), "weight")
	if err != nil {
		return err
	}
	stableService, err := getService(stableServiceName, services)
	if err != nil {
		return err
	}
	if stableService == nil {
		return errors.New("traefik stable service was not found")
	}
	err = unstructured.SetNestedField(stableService, int64(100-desiredWeight), "weight")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedSlice(traefikService.Object, services, "spec", "weighted", "services")
	if err != nil {
		return err
	}
	_, err = traefikServiceClient.Update(ctx, traefikService, metav1.UpdateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error updating traefik service %q: %s", traefikService.GetName(), err)
		r.sendWarningEvent(TraefikServiceUpdateError, msg)
	}
	return err
}

func getService(serviceName string, services []interface{}) (map[string]interface{}, error) {
	var selectedService map[string]interface{}
	for _, service := range services {
		typedService, ok := service.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion setting weight for traefik service")
		}
		nameOfCurrentService, isFound, err := unstructured.NestedString(typedService, "name")
		if err != nil {
			return nil, err
		}
		if !isFound {
			return nil, errors.New("name field was not found in service")
		}
		if nameOfCurrentService == serviceName {
			selectedService = typedService
			break
		}
	}
	return selectedService, nil
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	return nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

func (r *Reconciler) Type() string {
	return Type
}

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	ctx := context.TODO()
	if len(setMirrorRoute.Match) == 0 {
		err := r.removeMirrorRoute(setMirrorRoute)
		return err
	}
	ingressRouteClient := r.ClientSet.IngressRouteClient
	err := r.createMirrorTraefikService(setMirrorRoute)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}
	mirrorIngressRoute, err := ingressRouteClient.Get(ctx, MirrorIngressRouteName, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if k8serrors.IsNotFound(err) {
		err = r.createMirrorIngressRoute(setMirrorRoute)
		if err != nil {
			return err
		}
		return nil
	}
	err = r.updateMirrorIngressRoute(mirrorIngressRoute, setMirrorRoute)
	if err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) removeMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	err := r.removeIngressRouteRoute(setMirrorRoute.Name)
	if err != nil {
		return err
	}
	err = r.removeMirrorTraefikService(setMirrorRoute.Name)
	return err
}

func (r *Reconciler) removeIngressRouteRoute(routeName string) error {
	ctx := context.TODO()
	ingressRouteClient := r.ClientSet.IngressRouteClient
	ingressRoute, err := ingressRouteClient.Get(ctx, MirrorIngressRouteName, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if k8serrors.IsNotFound(err) {
		return nil
	}
	routeList, isFound, err := unstructured.NestedSlice(ingressRoute.Object, "spec", "routes")
	if err != nil {
		return err
	}
	if !isFound {
		return nil
	}
	newRouteList := []interface{}{}
	for _, route := range routeList {
		typedRoute, ok := route.(map[string]interface{})
		isCurrentRoute := false
		if !ok {
			return errors.New("Failed type assertion for route of Traefik IngressRoute")
		}
		if services, ok := typedRoute["services"]; ok {
			typedServices, ok := services.([]interface{})
			if !ok {
				return errors.New("Failed type assertion for route services of Traefik IngressRoute")
			}
			for _, service := range typedServices {
				typedService, ok := service.(map[string]interface{})
				if !ok {
					return errors.New("Failed type assertion for route services item of Traefik IngressRoute")
				}
				currentRouteName := typedService["name"]
				currentTypedRouteName, ok := currentRouteName.(string)
				if !ok {
					return errors.New("Failed type assertion for route service name of Traefik IngressRoute")
				}
				currentRouteKind := typedService["kind"]
				currentTypedRouteKind, ok := currentRouteKind.(string)
				if !ok {
					return errors.New("Failed type assertion for route service kind of Traefik IngressRoute")
				}
				if currentTypedRouteName == routeName && currentTypedRouteKind == "TraefikService" {
					isCurrentRoute = true
					break
				}
			}
		}
		if !isCurrentRoute {
			newRouteList = append(newRouteList, route)
		}
	}
	err = unstructured.SetNestedSlice(ingressRoute.Object, newRouteList, "spec", "routes")
	if err != nil {
		return err
	}
	_, err = ingressRouteClient.Update(ctx, ingressRoute, metav1.UpdateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error updating of mirror ingress route %q: %s", MirrorIngressRouteName, err)
		r.sendWarningEvent(IngressRouteUpdateError, msg)
	}
	return err
}

func (r *Reconciler) removeMirrorTraefikService(mirrorTraefikServiceName string) error {
	ctx := context.TODO()
	traefikServiceClient := r.ClientSet.TraefikServiceClient
	err := traefikServiceClient.Delete(ctx, mirrorTraefikServiceName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		msg := fmt.Sprintf("Error deleting mirror TraefikService %q: %s", mirrorTraefikServiceName, err)
		r.sendWarningEvent(TraefikServiceDeleteError, msg)
		return err
	}
	return nil
}

func (r *Reconciler) createMirrorTraefikService(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	ctx := context.TODO()
	traefikServiceClient := r.ClientSet.TraefikServiceClient
	traefikServiceName := r.Rollout.Spec.Strategy.Canary.TrafficRouting.Traefik.WeightedTraefikServiceName
	mirrorTraefikService, err := toUnstructured(MirrorTraefikService)
	if err != nil {
		return err
	}
	err = setMirrorTraefikServiceName(mirrorTraefikService, setMirrorRoute)
	if err != nil {
		return err
	}
	err = setTraefikServiceMirroring(mirrorTraefikService, traefikServiceName)
	if err != nil {
		return err
	}
	err = r.setTraefikServiceMirrors(mirrorTraefikService, setMirrorRoute)
	if err != nil {
		return err
	}
	_, err = traefikServiceClient.Create(ctx, mirrorTraefikService, metav1.CreateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error creating mirror traefik service %q: %s", mirrorTraefikService.GetName(), err)
		r.sendWarningEvent(TraefikServiceCreateError, msg)
	}
	return err
}

func setMirrorTraefikServiceName(mirrorTraefikService *unstructured.Unstructured, setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	err := unstructured.SetNestedField(mirrorTraefikService.Object, setMirrorRoute.Name, "metadata", "name")
	if err != nil {
		return err
	}
	return nil
}

func setTraefikServiceMirroring(mirrorTraefikService *unstructured.Unstructured, traefikServiceName string) error {
	mirroringConfig := map[string]interface{}{
		"kind": "TraefikService",
		"name": traefikServiceName,
	}
	err := unstructured.SetNestedMap(mirrorTraefikService.Object, mirroringConfig, "spec", "mirroring")
	if err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) setTraefikServiceMirrors(mirrorTraefikService *unstructured.Unstructured, setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	ctx := context.TODO()
	canaryServiceName := r.Rollout.Spec.Strategy.Canary.CanaryService
	serviceClient := r.ClientSet.ServiceClient
	canaryService, err := serviceClient.Get(ctx, canaryServiceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(canaryService.Spec.Ports) == 0 {
		return errors.New("Canary service \"ports\" field is empty")
	}
	if canaryService.Spec.Ports[0].Name == "" {
		return errors.New("Canary service \"ports\" field item doesn't have \"name\" field")
	}
	mirrorConfig := map[string]interface{}{
		"name":    canaryServiceName,
		"kind":    "Service",
		"port":    canaryService.Spec.Ports[0].Name,
		"percent": int64(*(setMirrorRoute.Percentage)),
	}
	mirrorList, isFound, err := unstructured.NestedSlice(mirrorTraefikService.Object, "spec", "mirroring", "mirrors")
	if err != nil {
		return err
	}
	if !isFound {
		mirrorList = []interface{}{}
	}
	mirrorList = append(mirrorList, mirrorConfig)
	err = unstructured.SetNestedSlice(mirrorTraefikService.Object, mirrorList, "spec", "mirroring", "mirrors")
	if err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) createMirrorIngressRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	ctx := context.TODO()
	ingressRouteClient := r.ClientSet.IngressRouteClient
	mirrorIngressRoute, err := toUnstructured(MirrorIngressRoute)
	if err != nil {
		return err
	}
	err = r.setMirrorIngressRouteRoute(mirrorIngressRoute, setMirrorRoute)
	if err != nil {
		return err
	}
	_, err = ingressRouteClient.Create(ctx, mirrorIngressRoute, metav1.CreateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error creating mirror IngressRoute %q: %s", mirrorIngressRoute.GetName(), err)
		r.sendWarningEvent(IngressRouteCreateError, msg)
	}
	return err
}

func (r *Reconciler) setMirrorIngressRouteRoute(mirrorIngressRoute *unstructured.Unstructured, setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	managedRoutes := r.Rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes
	ingressRouteMatch, err := getIngressRouteMatch(setMirrorRoute.Match)
	if err != nil {
		return err
	}
	routeConfig := map[string]interface{}{
		"match": ingressRouteMatch,
		"kind":  "Rule",
		"services": []interface{}{
			map[string]interface{}{
				"kind": "TraefikService",
				"name": setMirrorRoute.Name,
			},
		},
	}
	highestIngressRoutePriority, err := r.getHighestIngressRoutePriority()
	if err != nil {
		return err
	}
	managedRoutePriority := getManagedRoutePriority(setMirrorRoute, managedRoutes)
	routeConfig["priority"] = highestIngressRoutePriority + managedRoutePriority
	routeList, isFound, err := unstructured.NestedSlice(mirrorIngressRoute.Object, "spec", "routes")
	if err != nil {
		return err
	}
	if !isFound {
		routeList = []interface{}{}
	}
	routeList = append(routeList, routeConfig)
	err = unstructured.SetNestedSlice(mirrorIngressRoute.Object, routeList, "spec", "routes")
	return err
}

func getIngressRouteMatch(mirrorRouteMatch []v1alpha1.RouteMatch) (string, error) {
	ingressRouteMatch := strings.Builder{}
	var err error
	for index, match := range mirrorRouteMatch {
		if index > 0 {
			_, err = ingressRouteMatch.WriteString(" || ")
			if err != nil {
				return "", err
			}
		}
		_, err = ingressRouteMatch.WriteString(getPathIngressRouteMatch(&match))
		if err != nil {
			return "", err
		}
		if match.Method.Exact != "" {
			if ingressRouteMatch.Len() > 0 {
				_, err = ingressRouteMatch.WriteString(" && ")
				if err != nil {
					return "", err
				}
			}
			_, err = ingressRouteMatch.WriteString(fmt.Sprintf("Method(`%s`)", match.Method.Exact))
			if err != nil {
				return "", err
			}
		}
		for headerName, headerMatch := range match.Headers {
			if ingressRouteMatch.Len() > 0 {
				_, err = ingressRouteMatch.WriteString(" && ")
				if err != nil {
					return "", err
				}
			}
			_, err = ingressRouteMatch.WriteString(getHeadersIngressRouteMatch(headerName, &headerMatch))
			if err != nil {
				return "", err
			}
		}
	}
	if ingressRouteMatch.Len() == 0 {
		_, err = ingressRouteMatch.WriteString(DefaultMirrorIngressRouteMatch)
		if err != nil {
			return "", err
		}
	}
	return ingressRouteMatch.String(), nil
}

func getPathIngressRouteMatch(routeMatch *v1alpha1.RouteMatch) string {
	pathIngressRouteMatch := ""
	if routeMatch.Path.Exact != "" {
		pathIngressRouteMatch = fmt.Sprintf("Path(`%s`)", routeMatch.Path.Exact)
	} else if routeMatch.Path.Prefix != "" {
		pathIngressRouteMatch = fmt.Sprintf("PathPrefix(`%s`)", routeMatch.Path.Prefix)
	} else if routeMatch.Path.Regex != "" {
		pathIngressRouteMatch = fmt.Sprintf("Path(`%s`)", routeMatch.Path.Regex)
	}
	return pathIngressRouteMatch
}

func getHeadersIngressRouteMatch(headerName string, headerMatch *v1alpha1.StringMatch) string {
	headersIngressRouteMatch := ""
	if headerMatch.Exact != "" {
		headersIngressRouteMatch = fmt.Sprintf("Headers(`%s`, `%s`)", headerName, headerMatch.Exact)
	} else if headerMatch.Prefix != "" {
		headersIngressRouteMatch = fmt.Sprintf("HeadersRegexp(`%s`, `%s.*`)", headerName, headerMatch.Prefix)
	} else if headerMatch.Regex != "" {
		headersIngressRouteMatch = fmt.Sprintf("HeadersRegexp(`%s`, `%s`)", headerName, headerMatch.Regex)
	}
	return headersIngressRouteMatch
}

func (r *Reconciler) getHighestIngressRoutePriority() (int, error) {
	ctx := context.TODO()
	ingressRouteClient := r.ClientSet.IngressRouteClient
	ingressRouteList := r.Rollout.Spec.Strategy.Canary.TrafficRouting.Traefik.IngressRoutes
	highestIngressRoutePriority := MinimumIngressRoutePriority
	for _, ingressRouteData := range ingressRouteList {
		ingressRouteName := ingressRouteData.Name
		ingressRoute, err := ingressRouteClient.Get(ctx, ingressRouteName, metav1.GetOptions{})
		if err != nil {
			return highestIngressRoutePriority, nil
		}
		routeList, isFound, err := unstructured.NestedSlice(ingressRoute.Object, "spec", "routes")
		if err != nil {
			return highestIngressRoutePriority, err
		}
		if !isFound {
			return highestIngressRoutePriority, errors.New(fmt.Sprintf("IngressRoute \"%s\" is not found", ingressRouteName))
		}
		for _, route := range routeList {
			typedRoute, ok := route.(map[string]interface{})
			if !ok {
				return highestIngressRoutePriority, errors.New("Failed type assertion for route of Traefik IngressRoute")
			}
			if currentPriority, ok := typedRoute["priority"]; ok {
				typedCurrentPriority, ok := currentPriority.(int)
				if !ok {
					return highestIngressRoutePriority, errors.New("Failed type assertion for route priority of Traefik IngressRoute")
				}
				if typedCurrentPriority > highestIngressRoutePriority {
					highestIngressRoutePriority = typedCurrentPriority
				}
				continue
			}
			if match, ok := typedRoute["match"]; ok {
				typedMatch, ok := match.(string)
				if !ok {
					return highestIngressRoutePriority, errors.New("Failed type assertion for route match of Traefik IngressRoute")
				}
				currentPriority := len(typedMatch)
				if currentPriority > highestIngressRoutePriority {
					highestIngressRoutePriority = currentPriority
				}
			} else {
				return highestIngressRoutePriority, errors.New("IngressRoute \"route\" doesn't have \"match\" and \"priority\" fields")
			}
		}
	}
	return highestIngressRoutePriority, nil
}

func getManagedRoutePriority(setMirrorroute *v1alpha1.SetMirrorRoute, managedRoutes []v1alpha1.ManagedRoutes) int {
	for index, managedRoute := range managedRoutes {
		if managedRoute.Name == setMirrorroute.Name {
			return len(managedRoutes) - index
		}
	}
	return 0
}

func (r *Reconciler) updateMirrorIngressRoute(mirrorIngressRoute *unstructured.Unstructured, setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	ctx := context.TODO()
	ingressRouteClient := r.ClientSet.IngressRouteClient
	err := r.setMirrorIngressRouteRoute(mirrorIngressRoute, setMirrorRoute)
	if err != nil {
		return err
	}
	_, err = ingressRouteClient.Update(ctx, mirrorIngressRoute, metav1.UpdateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error updating IngressRoute %q: %s", mirrorIngressRoute.GetName(), err)
		r.sendWarningEvent(IngressRouteUpdateError, msg)
	}
	return err
}

func (r *Reconciler) RemoveManagedRoutes() error {
	ctx := context.TODO()
	managedRouteList := r.Rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes
	traefikServiceClient := r.ClientSet.TraefikServiceClient
	ingressRouteClient := r.ClientSet.IngressRouteClient
	err := ingressRouteClient.Delete(ctx, MirrorIngressRouteName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		msg := fmt.Sprintf("Error deleting mirror IngressRoute %q: %s", MirrorIngressRouteName, err)
		r.sendWarningEvent(IngressRouteDeleteError, msg)
		return err
	}
	for _, managedRoute := range managedRouteList {
		err = traefikServiceClient.Delete(ctx, managedRoute.Name, metav1.DeleteOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			msg := fmt.Sprintf("Error deleting mirror TraefikService %q: %s", managedRoute.Name, err)
			r.sendWarningEvent(TraefikServiceDeleteError, msg)
			return err
		}
	}
	return nil
}

func toUnstructured(manifest string) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode([]byte(manifest), nil, obj)
	if err != nil {
		return nil, err
	}
	return obj, nil
}
