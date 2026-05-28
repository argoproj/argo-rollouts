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

func (g *Given) RolloutTemplate(text string, values map[string]string) *Given {
	yamlBytes := g.yamlBytes(text)
	newText := string(yamlBytes)
	for k, v := range values {
		newText = strings.ReplaceAll(newText, k, v)
	}
	return g.RolloutObjects(newText)
}

func (g *Given) SetSteps(text string) *Given {
	steps := make([]rov1.CanaryStep, 0)
	err := yaml.Unmarshal([]byte(text), &steps)
	g.CheckError(err)
	var stepsUn []any
	for _, step := range steps {
		stepUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&step)
		g.CheckError(err)
		stepsUn = append(stepsUn, stepUn)
	}
	err = unstructured.SetNestedSlice(g.rollout.Object, stepsUn, "spec", "strategy", "canary", "steps")
	g.CheckError(err)
	return g
}

func (g *Given) RevisionHistoryLimit(limit int) *Given {
	err := unstructured.SetNestedField(g.rollout.Object, int64(limit), "spec", "revisionHistoryLimit")
	g.CheckError(err)
	return g
}

func (g *Given) ScaleDownDelaySeconds(seconds int) *Given {
	strategy, found, err := unstructured.NestedMap(g.rollout.Object, "spec", "strategy")
	g.CheckError(err)
	if !found {
		g.t.Fatal("strategy not found")
	}
	
	// Check if it's canary or blueGreen
	if _, ok := strategy["canary"]; ok {
		err = unstructured.SetNestedField(g.rollout.Object, int64(seconds), "spec", "strategy", "canary", "scaleDownDelaySeconds")
	} else if _, ok := strategy["blueGreen"]; ok {
		err = unstructured.SetNestedField(g.rollout.Object, int64(seconds), "spec", "strategy", "blueGreen", "scaleDownDelaySeconds")
	} else {
		g.t.Fatal("unknown strategy type")
	}
	g.CheckError(err)
	return g
}

func (g *Given) AutoPromotionSeconds(seconds int) *Given {
	err := unstructured.SetNestedField(g.rollout.Object, int64(seconds), "spec", "strategy", "blueGreen", "autoPromotionSeconds")
	g.CheckError(err)
	return g
}

func (g *Given) AutoPromotionEnabled(enabled bool) *Given {
	err := unstructured.SetNestedField(g.rollout.Object, enabled, "spec", "strategy", "blueGreen", "autoPromotionEnabled")
	g.CheckError(err)
	return g
}

func (g *Given) SetVersion(version string) *Given {
	err := unstructured.SetNestedField(g.rollout.Object, map[string]interface{}{"version": version}, "spec", "template", "metadata", "annotations")
	g.CheckError(err)
	return g
}

func (g *Given) SetRollbackWindow(revisions int) *Given {
	err := unstructured.SetNestedField(g.rollout.Object, int64(revisions), "spec", "rollbackWindow", "revisions")
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
