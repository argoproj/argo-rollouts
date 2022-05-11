package lint

import (
	"bytes"
	"encoding/json"
	"fmt"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	"io"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"unicode"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/validation"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	goyaml "gopkg.in/yaml.v2"
	istioNetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioNetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type LintOptions struct {
	options.ArgoRolloutsOptions
	File string
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

// isJSON detects if the byte array looks like json, based on the first non-whitespace character
func isJSON(fileBytes []byte) bool {
	for _, b := range fileBytes {
		if !unicode.IsSpace(rune(b)) {
			return b == '{'
		}
	}
	return false
}

func unmarshal(fileBytes []byte, obj interface{}) error {
	if isJSON(fileBytes) {
		decoder := json.NewDecoder(bytes.NewReader(fileBytes))
		decoder.DisallowUnknownFields()
		return decoder.Decode(&obj)
	}
	return yaml.UnmarshalStrict(fileBytes, &obj, yaml.DisallowUnknownFields)
}

func (l *LintOptions) lintResource(path string) error {
	fileBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	var un unstructured.Unstructured
	var fileRollouts []v1alpha1.Rollout
	var fileServices []validation.ServiceWithType
	var fileIngresses []ingressutil.Ingress
	var fileAnalysisTemplates []validation.AnalysisTemplatesWithType
	var fileVirtualServices []unstructured.Unstructured

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
		switch {
		case gvk.Group == rollouts.Group && gvk.Kind == rollouts.RolloutKind:
			var ro v1alpha1.Rollout
			err := unmarshal(valueBytes, &ro)
			if err != nil {
				return err
			}
			fileRollouts = append(fileRollouts, ro)

		case gvk.Group == v1.GroupName && gvk.Kind == "Service":
			var svc v1.Service
			err := unmarshal(valueBytes, &svc)
			if err != nil {
				return err
			}
			fileServices = append(fileServices, validation.ServiceWithType{
				Service: &svc,
			})

		case gvk.Group == istioNetworkingv1beta1.GroupName && gvk.Kind == "VirtualService":
			var virtualServicev1beta1 istioNetworkingv1beta1.VirtualService
			var virtualServicev1alpha3 istioNetworkingv1alpha3.VirtualService
			if gvk.Version == "v1alpha3" {
				err := unmarshal(valueBytes, &virtualServicev1alpha3)
				if err != nil {
					return err
				}

				vsvcUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&virtualServicev1alpha3)
				if err != nil {
					return err
				}
				fileVirtualServices = append(fileVirtualServices, unstructured.Unstructured{
					Object: vsvcUn,
				})
			} else if gvk.Version == "v1beta1" {
				err := unmarshal(valueBytes, &virtualServicev1beta1)
				if err != nil {
					return err
				}
				vsvcUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&virtualServicev1beta1)
				if err != nil {
					return err
				}
				fileVirtualServices = append(fileVirtualServices, unstructured.Unstructured{
					Object: vsvcUn,
				})
			}

		case (gvk.Group == networkingv1.GroupName || gvk.Group == extensionsv1beta1.GroupName) && gvk.Kind == "Ingress":
			var ing networkingv1.Ingress
			var ingv1beta1 extensionsv1beta1.Ingress
			if gvk.Version == "v1" {
				err := unmarshal(valueBytes, &ing)
				if err != nil {
					return err
				}
				fileIngresses = append(fileIngresses, *ingressutil.NewIngress(&ing))
			} else if gvk.Version == "v1beta1" {
				err := unmarshal(valueBytes, &ingv1beta1)
				if err != nil {
					return err
				}
				fileIngresses = append(fileIngresses, *ingressutil.NewLegacyIngress(&ingv1beta1))
			}

		}
	}
	
	refResource := validation.ReferencedResources{
		AnalysisTemplatesWithType: fileAnalysisTemplates,
		Ingresses:                 fileIngresses,
		ServiceWithType:           fileServices,
		VirtualServices:           fileVirtualServices,
		AmbassadorMappings:        nil,
		AppMeshResources:          nil,
	}

	setServiceTypeAndManagedAnnotation(fileRollouts, refResource)

	var errList field.ErrorList
	for _, rollout := range fileRollouts {
		errList = append(errList, validation.ValidateRollout(&rollout)...)
		errList = append(errList, validation.ValidateRolloutReferencedResources(&rollout, refResource)...)
	}

	for _, e := range errList {
		fmt.Println(e.ErrorBody())
	}
	if len(errList) > 1 {
		return errList[0]
	} else {
		return nil
	}
}

func setServiceTypeAndManagedAnnotation(ro []v1alpha1.Rollout, refResource validation.ReferencedResources) {
	//Go through and update all the services to be managed by their respective rollouts and set the type
	for _, rollout := range ro {
		for i, _ := range refResource.ServiceWithType {

			if refResource.ServiceWithType[i].Service.Annotations == nil {
				refResource.ServiceWithType[i].Service.Annotations = make(map[string]string)
			}
			refResource.ServiceWithType[i].Service.Annotations[v1alpha1.ManagedByRolloutsKey] = rollout.Name

			if rollout.Spec.Strategy.Canary != nil {
				if rollout.Spec.Strategy.Canary.CanaryService == refResource.ServiceWithType[i].Service.Name {
					refResource.ServiceWithType[i].Type = validation.CanaryService
				}
				if rollout.Spec.Strategy.Canary.StableService == refResource.ServiceWithType[i].Service.Name {
					refResource.ServiceWithType[i].Type = validation.StableService
				}
				if rollout.Spec.Strategy.Canary.PingPong != nil {
					if rollout.Spec.Strategy.Canary.PingPong.PingService == refResource.ServiceWithType[i].Service.Name {
						refResource.ServiceWithType[i].Type = validation.PingService
					}
					if rollout.Spec.Strategy.Canary.PingPong.PongService == refResource.ServiceWithType[i].Service.Name {
						refResource.ServiceWithType[i].Type = validation.PongService
					}
				}
			}
			if rollout.Spec.Strategy.BlueGreen != nil && rollout.Spec.Strategy.BlueGreen.ActiveService == refResource.ServiceWithType[i].Service.Name {
				refResource.ServiceWithType[i].Type = validation.ActiveService
			}
			if rollout.Spec.Strategy.BlueGreen != nil && rollout.Spec.Strategy.BlueGreen.PreviewService == refResource.ServiceWithType[i].Service.Name {
				refResource.ServiceWithType[i].Type = validation.PreviewService
			}
		}
	}
}
