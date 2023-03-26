package lint

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/validation"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	"github.com/spf13/cobra"
	goyaml "gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

type LintOptions struct {
	options.ArgoRolloutsOptions
	File string
}

type roAndReferences struct {
	Rollout    v1alpha1.Rollout
	References validation.ReferencedResources
}

const (
	lintExample = `
	# Lint a rollout
	%[1]s lint -f my-rollout.yaml`
)

// NewCmdLint returns a new instance of a `rollouts lint` command
func NewCmdLint(o *options.ArgoRolloutsOptions) *cobra.Command {
	lintOptions := LintOptions{
		ArgoRolloutsOptions: *o,
	}
	var cmd = &cobra.Command{
		Use:          "lint",
		Short:        "Lint and validate a Rollout",
		Long:         "This command lints and validates a new Rollout resource from a file.",
		Example:      o.Example(lintExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if lintOptions.File == "" {
				return o.UsageErr(c)
			}

			err := lintOptions.lintResource(lintOptions.File)
			if err != nil {
				return err
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&lintOptions.File, "filename", "f", "", "File to lint")
	return cmd
}

func unmarshal(fileBytes []byte, obj interface{}) error {
	return yaml.UnmarshalStrict(fileBytes, &obj, yaml.DisallowUnknownFields)
}

func (l *LintOptions) lintResource(path string) error {
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var un unstructured.Unstructured
	var refResource validation.ReferencedResources
	var fileRollouts []v1alpha1.Rollout

	decoder := goyaml.NewDecoder(bytes.NewReader(fileBytes))
	for {
		var value interface{}
		if err := decoder.Decode(&value); err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		if value == nil {
			continue
		}
		valueBytes, err := goyaml.Marshal(value)
		if err != nil {
			return err
		}

		if err = yaml.UnmarshalStrict(valueBytes, &un, yaml.DisallowUnknownFields); err != nil {
			return err
		}

		gvk := un.GroupVersionKind()
		if gvk.Group == rollouts.Group && gvk.Kind == rollouts.RolloutKind {
			var ro v1alpha1.Rollout
			err := unmarshal(valueBytes, &ro)
			if err != nil {
				return err
			}
			fileRollouts = append(fileRollouts, ro)
		}
		err = buildAllReferencedResources(un, &refResource)
		if err != nil {
			return err
		}
	}

	setServiceTypeAndManagedAnnotation(fileRollouts, refResource)
	setIngressManagedAnnotation(fileRollouts, refResource)
	setVirtualServiceManagedAnnotation(fileRollouts, refResource)

	var errList field.ErrorList
	for _, rollout := range fileRollouts {
		roRef := matchRolloutToReferences(rollout, refResource)

		errList = append(errList, validation.ValidateRollout(&roRef.Rollout)...)
		errList = append(errList, validation.ValidateRolloutReferencedResources(&roRef.Rollout, roRef.References)...)
	}

	for _, e := range errList {
		fmt.Println(e.ErrorBody())
	}
	if len(errList) > 0 {
		return errList[0]
	} else {
		return nil
	}
}

// buildAllReferencedResources This builds a ReferencedResources object that has all the external resources for every
// rollout resource in the manifest. We will need to later match each referenced resource to its own rollout resource
// before passing the rollout object and its managed reference on to validation.
func buildAllReferencedResources(un unstructured.Unstructured, refResource *validation.ReferencedResources) error {

	valueBytes, err := un.MarshalJSON()
	if err != nil {
		return err
	}

	gvk := un.GroupVersionKind()
	switch {
	case gvk.Group == v1.GroupName && gvk.Kind == "Service":
		var svc v1.Service
		err := unmarshal(valueBytes, &svc)
		if err != nil {
			return err
		}
		refResource.ServiceWithType = append(refResource.ServiceWithType, validation.ServiceWithType{
			Service: &svc,
		})

	case gvk.Group == "networking.istio.io" && gvk.Kind == "VirtualService":
		refResource.VirtualServices = append(refResource.VirtualServices, un)

	case (gvk.Group == networkingv1.GroupName || gvk.Group == extensionsv1beta1.GroupName) && gvk.Kind == "Ingress":
		var ing networkingv1.Ingress
		var ingv1beta1 extensionsv1beta1.Ingress
		if gvk.Version == "v1" {
			err := unmarshal(valueBytes, &ing)
			if err != nil {
				return err
			}
			refResource.Ingresses = append(refResource.Ingresses, *ingressutil.NewIngress(&ing))
		} else if gvk.Version == "v1beta1" {
			err := unmarshal(valueBytes, &ingv1beta1)
			if err != nil {
				return err
			}
			refResource.Ingresses = append(refResource.Ingresses, *ingressutil.NewLegacyIngress(&ingv1beta1))
		}

	}
	return nil
}

// matchRolloutToReferences This function goes through the global list of all ReferencedResources in the manifest and matches
// them up with their respective rollout object so that we can latter have a mapping of a single rollout object and its
// referenced resources.
func matchRolloutToReferences(rollout v1alpha1.Rollout, refResource validation.ReferencedResources) roAndReferences {
	matchedReferenceResources := roAndReferences{Rollout: rollout, References: validation.ReferencedResources{}}

	for _, service := range refResource.ServiceWithType {
		if service.Service.Annotations[v1alpha1.ManagedByRolloutsKey] == rollout.Name {
			matchedReferenceResources.References.ServiceWithType = append(matchedReferenceResources.References.ServiceWithType, service)
		}
	}
	for _, ingress := range refResource.Ingresses {
		if ingress.GetAnnotations()[v1alpha1.ManagedByRolloutsKey] == rollout.Name {
			matchedReferenceResources.References.Ingresses = append(matchedReferenceResources.References.Ingresses, ingress)
		}
	}
	for _, virtualService := range refResource.VirtualServices {
		if virtualService.GetAnnotations()[v1alpha1.ManagedByRolloutsKey] == rollout.Name {
			matchedReferenceResources.References.VirtualServices = append(matchedReferenceResources.References.VirtualServices, virtualService)
		}
	}

	return matchedReferenceResources
}

// setServiceTypeAndManagedAnnotation This sets the managed annotation on each service as well as figures out what
// type of service its is by looking at the rollout and set's its service type accordingly.
func setServiceTypeAndManagedAnnotation(rollouts []v1alpha1.Rollout, refResource validation.ReferencedResources) {
	for _, rollout := range rollouts {
		for i := range refResource.ServiceWithType {

			if refResource.ServiceWithType[i].Service.Annotations == nil {
				refResource.ServiceWithType[i].Service.Annotations = make(map[string]string)
			}

			if rollout.Spec.Strategy.Canary != nil {
				if rollout.Spec.Strategy.Canary.CanaryService == refResource.ServiceWithType[i].Service.Name {
					refResource.ServiceWithType[i].Type = validation.CanaryService
					refResource.ServiceWithType[i].Service.Annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
				}
				if rollout.Spec.Strategy.Canary.StableService == refResource.ServiceWithType[i].Service.Name {
					refResource.ServiceWithType[i].Type = validation.StableService
					refResource.ServiceWithType[i].Service.Annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
				}
				if rollout.Spec.Strategy.Canary.PingPong != nil {
					if rollout.Spec.Strategy.Canary.PingPong.PingService == refResource.ServiceWithType[i].Service.Name {
						refResource.ServiceWithType[i].Type = validation.PingService
						refResource.ServiceWithType[i].Service.Annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
					}
					if rollout.Spec.Strategy.Canary.PingPong.PongService == refResource.ServiceWithType[i].Service.Name {
						refResource.ServiceWithType[i].Type = validation.PongService
						refResource.ServiceWithType[i].Service.Annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
					}
				}
			}

			if rollout.Spec.Strategy.BlueGreen != nil {
				if rollout.Spec.Strategy.BlueGreen.ActiveService == refResource.ServiceWithType[i].Service.Name {
					refResource.ServiceWithType[i].Type = validation.ActiveService
					refResource.ServiceWithType[i].Service.Annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
				}
				if rollout.Spec.Strategy.BlueGreen.PreviewService == refResource.ServiceWithType[i].Service.Name {
					refResource.ServiceWithType[i].Type = validation.PreviewService
					refResource.ServiceWithType[i].Service.Annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
				}
			}

		}
	}
}

// setIngressManagedAnnotation This tries to find ingresses that have matching services in the rollout resource and if so
// it will add the managed by annotations just for linting so that we can later match up resources to a rollout resources
// for the case when we have multiple rollout resources in a single manifest.
func setIngressManagedAnnotation(rollouts []v1alpha1.Rollout, refResource validation.ReferencedResources) {
	for _, rollout := range rollouts {
		for i := range refResource.Ingresses {
			var serviceName string

			// Basic Canary so ingress is only pointing a single service and so no linting is needed for this case.
			if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.TrafficRouting == nil {
				return
			}
			if rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil {
				serviceName = rollout.Spec.Strategy.Canary.StableService
			} else if rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil {
				serviceName = rollout.Spec.Strategy.Canary.StableService
				if rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService != "" {
					serviceName = rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService
				}
			} else if rollout.Spec.Strategy.Canary.TrafficRouting.SMI != nil {
				serviceName = rollout.Spec.Strategy.Canary.TrafficRouting.SMI.RootService
			}

			if ingressutil.HasRuleWithService(&refResource.Ingresses[i], serviceName) {
				annotations := refResource.Ingresses[i].GetAnnotations()
				if annotations == nil {
					annotations = make(map[string]string)
				}
				annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
				refResource.Ingresses[i].SetAnnotations(annotations)
			}
		}
	}
}

// setVirtualServiceManagedAnnotation This function finds virtual services that are listed in the rollout resources and
// adds the ManagedByRolloutsKey to the annotations of the virtual services.
func setVirtualServiceManagedAnnotation(ro []v1alpha1.Rollout, refResource validation.ReferencedResources) {
	for _, rollout := range ro {
		for i := range refResource.VirtualServices {
			if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.TrafficRouting == nil || rollout.Spec.Strategy.Canary.TrafficRouting.Istio == nil {
				return
			}
			if rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService != nil && rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name == refResource.VirtualServices[i].GetName() {
				annotations := refResource.VirtualServices[i].GetAnnotations()
				if annotations == nil {
					annotations = make(map[string]string)
				}
				annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
				refResource.VirtualServices[i].SetAnnotations(annotations)
			}
			for _, virtualService := range rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices {
				if virtualService.Name == refResource.VirtualServices[i].GetName() {
					annotations := refResource.VirtualServices[i].GetAnnotations()
					if annotations == nil {
						annotations = make(map[string]string)
					}
					annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name
					refResource.VirtualServices[i].SetAnnotations(annotations)
				}
			}
		}
	}
}
