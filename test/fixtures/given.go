package fixtures

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

var nonAlphanumericDash = regexp.MustCompile(`[^a-z0-9-]`)

// rolloutPluginUniqueName derives a DNS-label-safe Kubernetes resource name from the test name.
// Using the test function name as the resource name guarantees uniqueness across parallel tests
// without requiring per-test boilerplate in the YAML.
func rolloutPluginUniqueName(t *testing.T) string {
	name := t.Name()
	// Take just the test function name (after the last "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.ToLower(name)
	name = nonAlphanumericDash.ReplaceAllString(name, "-")
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

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

// RolloutPluginObjects sets up the rolloutplugin objects for the test environment given a YAML string or file path:
// 1. A file name if it starts with "@"
// 2. Raw YAML.
//
// The RolloutPlugin and its associated StatefulSet are automatically renamed to a unique name
// derived from the current test function name. This allows tests to run in parallel without
// resource name conflicts while keeping per-test YAML boilerplate minimal.
func (g *Given) RolloutPluginObjects(text string) *Given {
	g.t.Helper()
	uniqueName := rolloutPluginUniqueName(g.t)
	objs := g.parseTextToObjects(text)
	for _, obj := range objs {
		if obj.GetKind() == "RolloutPlugin" {
			if g.rolloutPlugin != nil {
				g.t.Fatal("multiple rolloutplugins specified")
			}
			obj.SetName(uniqueName)
			if err := unstructured.SetNestedField(obj.Object, uniqueName, "spec", "workloadRef", "name"); err != nil {
				g.t.Fatalf("failed to set workloadRef.name: %v", err)
			}
			g.log = g.log.WithField("rolloutplugin", uniqueName)
			g.rolloutPlugin = obj
		}
		if obj.GetKind() == "StatefulSet" {
			if g.statefulSet != nil {
				g.t.Fatal("multiple statefulsets specified")
			}
			obj.SetName(uniqueName)
			// serviceName must match the StatefulSet name for DNS headless service resolution
			if err := unstructured.SetNestedField(obj.Object, uniqueName, "spec", "serviceName"); err != nil {
				g.t.Fatalf("failed to set serviceName: %v", err)
			}
			// Rename pod selector and template labels so parallel tests don't select each other's pods
			if err := unstructured.SetNestedField(obj.Object, uniqueName, "spec", "selector", "matchLabels", "app"); err != nil {
				g.t.Fatalf("failed to set selector.matchLabels.app: %v", err)
			}
			if err := unstructured.SetNestedField(obj.Object, uniqueName, "spec", "template", "metadata", "labels", "app"); err != nil {
				g.t.Fatalf("failed to set template.metadata.labels.app: %v", err)
			}
			g.statefulSet = obj
		}
		g.objects = append(g.objects, obj)
	}
	return g
}

// SetRolloutPluginSteps dynamically sets canary steps on the rolloutplugin object.
func (g *Given) SetRolloutPluginSteps(text string) *Given {
	steps := make([]rov1.CanaryStep, 0)
	err := yaml.Unmarshal([]byte(text), &steps)
	g.CheckError(err)
	var stepsUn []any
	for _, step := range steps {
		stepUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&step)
		g.CheckError(err)
		stepsUn = append(stepsUn, stepUn)
	}
	err = unstructured.SetNestedSlice(g.rolloutPlugin.Object, stepsUn, "spec", "strategy", "canary", "steps")
	g.CheckError(err)
	return g
}

// HealthyRolloutPlugin is a convenience around creating a rolloutplugin and waiting for it to become healthy.
func (g *Given) HealthyRolloutPlugin(text string, steps ...string) *Given {
	given := g.RolloutPluginObjects(text)
	if len(steps) > 0 {
		given = given.SetRolloutPluginSteps(steps[0])
	}
	return given.
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		Given()
}

func (g *Given) When() *When {
	return &When{
		Common: g.Common,
	}
}
