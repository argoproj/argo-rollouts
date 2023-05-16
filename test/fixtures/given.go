package fixtures

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type Given struct {
	*Common
}

// RolloutObjects sets up the rollout objects for the test environment given a YAML string or file path:
// 1. A file name if it starts with "@"
// 2. Raw YAML.
func (g *Given) RolloutObjects(text string) *Given {
	g.t.Helper()
	objs := g.parseTextToObjects(text)
	for _, obj := range objs {
		if obj.GetKind() == "Rollout" {
			if g.rollout != nil {
				g.t.Fatal("multiple rollouts specified")
			}
			g.log = g.log.WithField("rollout", obj.GetName())
			g.rollout = obj
		}
		g.objects = append(g.objects, obj)
	}
	return g
}

func (g *Given) RolloutTemplate(text, name string) *Given {
	yamlBytes := g.yamlBytes(text)
	newText := strings.ReplaceAll(string(yamlBytes), "REPLACEME", name)
	return g.RolloutObjects(newText)
}

func (g *Given) SetSteps(text string) *Given {
	steps := make([]rov1.CanaryStep, 0)
	err := yaml.Unmarshal([]byte(text), &steps)
	g.CheckError(err)
	var stepsUn []interface{}
	for _, step := range steps {
		stepUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&step)
		g.CheckError(err)
		stepsUn = append(stepsUn, stepUn)
	}
	err = unstructured.SetNestedSlice(g.rollout.Object, stepsUn, "spec", "strategy", "canary", "steps")
	g.CheckError(err)
	return g
}

// HealthyRollout is a convenience around creating a rollout and waiting for it to become healthy
func (g *Given) HealthyRollout(text string) *Given {
	return g.RolloutObjects(text).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Given()
}

func (g *Given) StartEventWatch(ctx context.Context) *Given {
	g.Common.StartEventWatch(ctx)
	return g
}

func (g *Given) When() *When {
	return &When{
		Common: g.Common,
	}
}
