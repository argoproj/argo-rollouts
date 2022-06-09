package ambassador

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

// Type defines the ambassador traffic routing type.
const (
	Type                         = "Ambassador"
	AmbassadorMappingNotFound    = "AmbassadorMappingNotFound"
	AmbassadorMappingConfigError = "AmbassadorMappingConfigError"
	CanaryMappingCleanupError    = "CanaryMappingCleanupError"
	CanaryMappingCreationError   = "CanaryMappingCreationError"
	CanaryMappingUpdateError     = "CanaryMappingUpdateError"
	CanaryMappingWeightUpdate    = "CanaryMappingWeightUpdate"
)

var (
	apiGroupToResource = map[string]string{
		"getambassador.io":   "mappings",
		"x.getambassador.io": "ambassadormappings",
	}
)

// Reconciler implements a TrafficRoutingReconciler for Ambassador.
type Reconciler struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder record.EventRecorder
	Log      *logrus.Entry
}

// ClientInterface defines a subset of k8s client operations having only the required
// ones.
type ClientInterface interface {
	Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
	Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error
}

// NewDynamicClient will initialize a real kubernetes dynamic client to interact
// with Ambassador CRDs
func NewDynamicClient(di dynamic.Interface, namespace string) dynamic.ResourceInterface {
	return di.Resource(GetMappingGVR()).Namespace(namespace)
}

// NewReconciler will build and return an ambassador Reconciler
func NewReconciler(r *v1alpha1.Rollout, c ClientInterface, rec record.EventRecorder) *Reconciler {
	return &Reconciler{
		Rollout:  r,
		Client:   c,
		Recorder: rec,
		Log:      logutil.WithRollout(r),
	}
}

// SetWeight will configure a canary ambassador mapping with the given desiredWeight.
// The canary ambassador mapping is dynamically created cloning the mapping provided
// in the ambassador configuration in the traffic routing section of the rollout. If
// the canary ambassador mapping is already present, it will be updated to the given
// desiredWeight.
func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	r.sendNormalEvent(CanaryMappingWeightUpdate, fmt.Sprintf("Set canary mapping weight to %d", desiredWeight))
	ctx := context.TODO()
	baseMappingNameList := r.Rollout.Spec.Strategy.Canary.TrafficRouting.Ambassador.Mappings
	doneChan := make(chan struct{})
	errChan := make(chan error)
	defer close(errChan)
	var wg sync.WaitGroup
	wg.Add(len(baseMappingNameList))
	for _, baseMappingName := range baseMappingNameList {
		go func(baseMappingName string) {
			defer wg.Done()
			err := r.handleCanaryMapping(ctx, baseMappingName, desiredWeight)
			if err != nil {
				errChan <- err
			}
		}(baseMappingName)
	}
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	errs := []error{}
	done := false
	for !done {
		select {
		case <-doneChan:
			done = true
		case err := <-errChan:
			errs = append(errs, err)
		}
	}
	return formatErrors(errs)
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	return nil
}

func formatErrors(errs []error) error {
	errorsCount := len(errs)
	if errorsCount == 0 {
		return nil
	} else if errorsCount == 1 {
		return errs[0]
	}
	var errMsg strings.Builder
	errMsg.WriteString(fmt.Sprintf("%d errors found: ", errorsCount))
	for _, err := range errs {
		errMsg.WriteString(fmt.Sprintf("[%s]", err))
	}
	return errors.New(errMsg.String())
}

// handleCanaryMapping has the logic to create, update or delete canary mappings
func (r *Reconciler) handleCanaryMapping(ctx context.Context, baseMappingName string, desiredWeight int32) error {
	canaryMappingName := buildCanaryMappingName(baseMappingName)
	canaryMapping, err := r.Client.Get(ctx, canaryMappingName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if desiredWeight == 0 {
				return nil
			}
			r.Log.Infof("creating canary mapping based on %q", baseMappingName)
			return r.createCanaryMapping(ctx, baseMappingName, desiredWeight, r.Client)
		}
		return err
	}

	if desiredWeight == 0 {
		defer func() {
			// add buffer before scale down replica set
			r.Log.Infof("sleep %d sec for propagation of mapping update", 5)
			time.Sleep(5 * time.Second)
			// The deletion of the canary mapping needs to happen moments after
			// updating the weight to zero to prevent traffic to reach the older
			// version at the end of the rollout
			r.Log.Infof("deleting canary mapping %q", canaryMapping.GetName())
			err := r.deleteCanaryMapping(ctx, canaryMapping, desiredWeight, r.Client)
			if err != nil {
				r.Log.Errorf("error deleting canary mapping: %s", err)
			}
		}()
	}

	r.Log.Infof("updating canary mapping %q weight to %d", canaryMapping.GetName(), desiredWeight)
	return r.updateCanaryMapping(ctx, canaryMapping, desiredWeight, r.Client)
}

func (r *Reconciler) updateCanaryMapping(ctx context.Context,
	canaryMapping *unstructured.Unstructured,
	desiredWeight int32,
	client ClientInterface) error {

	setMappingWeight(canaryMapping, desiredWeight)
	_, err := client.Update(ctx, canaryMapping, metav1.UpdateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error updating canary mapping %q: %s", canaryMapping.GetName(), err)
		r.sendWarningEvent(CanaryMappingUpdateError, msg)
	}
	return err
}

func (r *Reconciler) deleteCanaryMapping(ctx context.Context,
	canaryMapping *unstructured.Unstructured,
	desiredWeight int32,
	client ClientInterface) error {

	err := client.Delete(ctx, canaryMapping.GetName(), metav1.DeleteOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error deleting canary mapping %q: %s", canaryMapping.GetName(), err)
		r.sendWarningEvent(CanaryMappingCleanupError, msg)
		return err
	}
	return nil
}

func (r *Reconciler) createCanaryMapping(ctx context.Context,
	baseMappingName string,
	desiredWeight int32,
	client ClientInterface) error {

	baseMapping, err := client.Get(ctx, baseMappingName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			msg := fmt.Sprintf("Ambassador mapping %q not found", baseMappingName)
			r.sendWarningEvent(AmbassadorMappingNotFound, msg)
		}
		return err
	}
	weight := GetMappingWeight(baseMapping)
	if weight != 0 {
		msg := fmt.Sprintf("Ambassador mapping %q can not define weight", baseMappingName)
		r.sendWarningEvent(AmbassadorMappingConfigError, msg)
		return fmt.Errorf(msg)
	}

	canarySvc := r.Rollout.Spec.Strategy.Canary.CanaryService
	canaryMapping := buildCanaryMapping(baseMapping, canarySvc, desiredWeight)
	_, err = client.Create(ctx, canaryMapping, metav1.CreateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error creating canary mapping: %s", err)
		r.sendWarningEvent(CanaryMappingCreationError, msg)
	}
	return err
}

func buildCanaryMapping(baseMapping *unstructured.Unstructured, canarySvc string, desiredWeight int32) *unstructured.Unstructured {
	canaryMapping := baseMapping.DeepCopy()
	svc := buildCanaryService(baseMapping, canarySvc)
	unstructured.RemoveNestedField(canaryMapping.Object, "metadata")
	cMappingName := buildCanaryMappingName(baseMapping.GetName())
	canaryMapping.SetName(cMappingName)
	canaryMapping.SetNamespace(baseMapping.GetNamespace())
	unstructured.SetNestedField(canaryMapping.Object, svc, "spec", "service")
	setMappingWeight(canaryMapping, desiredWeight)
	return canaryMapping
}

func buildCanaryService(baseMapping *unstructured.Unstructured, canarySvc string) string {
	curSvc := GetMappingService(baseMapping)
	parts := strings.Split(curSvc, ":")
	if len(parts) < 2 {
		return canarySvc
	}
	// Check if the last part is a valid int that can be used as the port
	port := parts[len(parts)-1]
	if _, err := strconv.Atoi(port); err != nil {
		return canarySvc

	}
	return fmt.Sprintf("%s:%s", canarySvc, port)
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

func (r *Reconciler) Type() string {
	return Type
}

func setMappingWeight(obj *unstructured.Unstructured, weight int32) {
	unstructured.SetNestedField(obj.Object, int64(weight), "spec", "weight")
}

func GetMappingWeight(obj *unstructured.Unstructured) int64 {
	weight, found, err := unstructured.NestedInt64(obj.Object, "spec", "weight")
	if err != nil || !found {
		return 0
	}
	return weight
}

func GetMappingService(obj *unstructured.Unstructured) string {
	svc, found, err := unstructured.NestedString(obj.Object, "spec", "service")
	if err != nil || !found {
		return ""
	}
	return svc
}

func buildCanaryMappingName(name string) string {
	n := name
	if len(name) > 246 {
		n = name[:246]
	}
	return fmt.Sprintf("%s-canary", n)
}

// GetMappingGVR will return the Ambassador Mapping GVR to be used. The logic is based on the
// ambassadorAPIVersion variable that is set with a default value. The default value can be
// changed by invoking the SetAPIVersion function.
func GetMappingGVR() schema.GroupVersionResource {
	return toMappingGVR(defaults.GetAmbassadorAPIVersion())
}

func toMappingGVR(apiVersion string) schema.GroupVersionResource {
	parts := strings.Split(apiVersion, "/")
	group := defaults.DefaultAmbassadorAPIGroup
	if len(parts) > 1 {
		group = parts[0]
	}
	resourcename, known := apiGroupToResource[group]
	if !known {
		resourcename = apiGroupToResource[defaults.DefaultAmbassadorAPIGroup]
	}
	version := parts[len(parts)-1]
	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resourcename,
	}
}

func (r *Reconciler) sendNormalEvent(id, msg string) {
	r.sendEvent(corev1.EventTypeNormal, id, msg)
}

func (r *Reconciler) sendWarningEvent(id, msg string) {
	r.sendEvent(corev1.EventTypeWarning, id, msg)
}

func (r *Reconciler) sendEvent(eventType, id, msg string) {
	r.Recorder.Eventf(r.Rollout, record.EventOptions{EventType: eventType, EventReason: id}, msg)
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	return nil
}

func (r *Reconciler) RemoveManagedRoutes() error {
	return nil
}
