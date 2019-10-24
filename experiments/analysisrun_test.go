package experiments

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func generateAnalysisTemplates(names ...string) []v1alpha1.AnalysisTemplate {
	var templates []v1alpha1.AnalysisTemplate
	for _, name := range names {
		t := v1alpha1.AnalysisTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: metav1.NamespaceDefault,
			},
			Spec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name: "job",
					},
				},
			},
		}
		templates = append(templates, t)
	}
	return templates
}

func analysisTemplateToRun(name string, ex *v1alpha1.Experiment, template *v1alpha1.AnalysisTemplate) *v1alpha1.AnalysisRun {
	ar := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    fmt.Sprintf("%s-%s-", ex.Name, name),
			Namespace:       ex.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ex, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: template.Spec,
		},
	}
	return &ar
}

// TestDontStartAnalysisRun verifies we don't create runs unless we are available
func TestDontStartAnalysisRunIfNotAvailable(t *testing.T) {
	templates := generateTemplates("bar")
	aTemplates := generateAnalysisTemplates("success-rate")
	e := newExperiment("foo", templates, nil)
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{
		{
			Name:         "success-rate",
			TemplateName: aTemplates[0].Name,
		},
	}
	rs := templateToRS(e, templates[0], 0)

	f := newFixture(t, e, rs, &aTemplates[0])
	defer f.Close()

	f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
}

// TestCreateAnalysisRunWhenAvailable ensures we create the AnalysisRun when we become available
func TestCreateAnalysisRunWhenAvailable(t *testing.T) {
	templates := generateTemplates("bar")
	aTemplates := generateAnalysisTemplates("success-rate")
	e := newExperiment("foo", templates, nil)
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{
		{
			Name:         "success-rate",
			TemplateName: aTemplates[0].Name,
		},
	}
	e.Status.Status = v1alpha1.AnalysisStatusRunning
	e.Status.AvailableAt = now()
	rs := templateToRS(e, templates[0], 1)
	ar := analysisTemplateToRun("success-rate", e, &aTemplates[0])

	f := newFixture(t, e, rs, &aTemplates[0])
	defer f.Close()

	f.expectCreateAnalysisRunAction(ar)
	patchIdx := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))

	patchedEx := f.getPatchedExperimentAsObj(patchIdx)
	assert.Equal(t, v1alpha1.AnalysisStatusPending, patchedEx.Status.AnalysisRuns[0].Status)
}

// TestAnalysisTemplateNotExists verifies we error the run the template does not exist (before availablility)
func TestAnalysisTemplateNotExists(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil)
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{
		{
			Name:         "success-rate",
			TemplateName: "does-not-exist",
		},
	}
	rs := templateToRS(e, templates[0], 1)

	f := newFixture(t, e, rs)
	defer f.Close()

	patchIdx := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))

	patchedEx := f.getPatchedExperimentAsObj(patchIdx)
	assert.Equal(t, v1alpha1.AnalysisStatusError, patchedEx.Status.AnalysisRuns[0].Status)
	assert.Contains(t, patchedEx.Status.AnalysisRuns[0].Message, "not found")
}

// TestAnalysisRunCreateError verifies we error the run if create fails
func TestAnalysisRunCreateError(t *testing.T) {
	templates := generateTemplates("bar")
	aTemplates := generateAnalysisTemplates("success-rate")
	e := newExperiment("foo", templates, nil)
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{
		{
			Name:         "success-rate",
			TemplateName: aTemplates[0].Name,
		},
	}
	e.Status.Status = v1alpha1.AnalysisStatusRunning
	e.Status.AvailableAt = now()
	rs := templateToRS(e, templates[0], 1)
	ar := analysisTemplateToRun("success-rate", e, &aTemplates[0])

	f := newFixture(t, e, rs, &aTemplates[0])
	defer f.Close()

	f.client.PrependReactor("create", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("intentional error")
	})

	f.expectCreateAnalysisRunAction(ar)
	patchIdx := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patchedEx := f.getPatchedExperimentAsObj(patchIdx)
	assert.Equal(t, v1alpha1.AnalysisStatusError, patchedEx.Status.AnalysisRuns[0].Status)
	assert.Contains(t, patchedEx.Status.AnalysisRuns[0].Message, "intentional error")
}
