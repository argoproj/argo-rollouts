package fixtures

import (
	"io/ioutil"
	"strconv"
	"strings"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type Given struct {
	Common
	rollout *rov1.Rollout
}

// Rollout sets up the rollout objects for the test environment given a YAML string or file path:
// 1. A file name if it starts with "@"
// 2. Raw YAML.
func (g *Given) Rollout(text string) *Given {
	g.t.Helper()
	yamlBytes := g.yamlBytes(text)
	objs, err := unstructuredutil.SplitYAML(string(yamlBytes))
	g.CheckError(err)
	for _, obj := range objs {
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[rov1.LabelKeyControllerInstanceID] = E2ELabel
		obj.SetLabels(labels)

		if obj.GetKind() == "Rollout" {
			if g.rollout != nil {
				g.t.Fatal("multiple rollouts specified")
			}
			g.rollout = &rov1.Rollout{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &g.rollout)
			g.CheckError(err)
			g.log = g.log.WithField("rollout", g.rollout.Name)

			// Modify pod termination delay if set
			if g.podDelay > 0 {
				g.rollout.Spec.Template.Spec.Containers[0].Args = []string{"--termination-delay", strconv.Itoa(g.podDelay)}
				g.rollout.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					InitialDelaySeconds: int32(g.podDelay),
					Handler: corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(8080),
						},
					},
				}
			}
		} else {
			// other non-rollout objects
			g.objects = append(g.objects, obj)
		}
	}
	if g.rollout == nil {
		g.t.Fatal("rollout not in objects")
	}
	return g
}

func (g *Given) RolloutTemplate(text, name string) *Given {
	yamlBytes := g.yamlBytes(text)
	newText := strings.ReplaceAll(string(yamlBytes), "REPLACEME", name)
	return g.Rollout(newText)
}

func (g *Given) yamlBytes(text string) []byte {
	var yamlBytes []byte
	var err error
	if strings.HasPrefix(text, "@") {
		file := strings.TrimPrefix(text, "@")
		yamlBytes, err = ioutil.ReadFile(file)
		g.CheckError(err)
	} else {
		yamlBytes = []byte(text)
	}
	return yamlBytes
}

func (g *Given) SetSteps(text string) *Given {
	steps := make([]rov1.CanaryStep, 0)
	err := yaml.Unmarshal([]byte(text), &steps)
	g.CheckError(err)
	g.rollout.Spec.Strategy.Canary.Steps = steps
	return g
}

// HealthyRollout is a convenience around creating a rollout and waiting for it to become healthy
func (g *Given) HealthyRollout(text string) *Given {
	return g.Rollout(text).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Given()
}

func (g *Given) When() *When {
	return &When{
		Common:  g.Common,
		rollout: g.rollout,
	}
}
