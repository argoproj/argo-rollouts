package experiments

import (
	"errors"
	"fmt"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

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

func analysisTemplateToRun(name string, ex *v1alpha1.Experiment, spec *v1alpha1.AnalysisTemplateSpec) *v1alpha1.AnalysisRun {
	ar := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    fmt.Sprintf("%s-%s-", ex.Name, name),
			Namespace:       ex.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ex, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: *spec,
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
	ar := analysisTemplateToRun("success-rate", e, &aTemplates[0].Spec)

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
	ar := analysisTemplateToRun("success-rate", e, &aTemplates[0].Spec)

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

func TestAnalysisRunSuccessful(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil)
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{
		{
			Name:         "success-rate",
			TemplateName: "success-rate",
		},
	}
	e.Status.Status = v1alpha1.AnalysisStatusRunning
	e.Status.AvailableAt = now()
	rs := templateToRS(e, templates[0], 1)
	ar := analysisTemplateToRun("success-rate", e, &v1alpha1.AnalysisTemplateSpec{})
	ar.Name = ar.GenerateName + "abc123"
	ar.Status = v1alpha1.AnalysisRunStatus{
		Status: v1alpha1.AnalysisStatusSuccessful,
	}
	e.Status.AnalysisRuns = []v1alpha1.ExperimentAnalysisRunStatus{
		{
			Name:        e.Spec.Analyses[0].Name,
			Status:      v1alpha1.AnalysisStatusRunning,
			AnalysisRun: ar.Name,
		},
	}

	f := newFixture(t, e, rs, ar)
	defer f.Close()

	patchIdx := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patchedEx := f.getPatchedExperimentAsObj(patchIdx)
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, patchedEx.Status.AnalysisRuns[0].Status)
}

func TestAssessAnalysisRunStatusesAfterTemplateSuccess(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil)
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{
		{
			Name:         "success-rate",
			TemplateName: "success-rate",
		},
		{
			Name:         "latency",
			TemplateName: "latency",
		},
	}
	e.Status.Status = v1alpha1.AnalysisStatusRunning
	e.Spec.Duration = pointer.Int32Ptr(60)
	e.Status.AvailableAt = secondsAgo(61)
	rs := templateToRS(e, templates[0], 0)
	rs.Spec.Replicas = new(int32)
	ar1 := analysisTemplateToRun("success-rate", e, &v1alpha1.AnalysisTemplateSpec{})
	ar1.Name = ar1.GenerateName + "abc123"
	ar2 := analysisTemplateToRun("latency", e, &v1alpha1.AnalysisTemplateSpec{})
	ar2.Name = ar2.GenerateName + "abc123"

	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		{
			Name:   e.Spec.Templates[0].Name,
			Status: v1alpha1.TemplateStatusSuccessful,
		},
	}
	e.Status.AnalysisRuns = []v1alpha1.ExperimentAnalysisRunStatus{
		{
			Name:        e.Spec.Analyses[0].Name,
			AnalysisRun: ar1.Name,
		},
		{
			Name:        e.Spec.Analyses[1].Name,
			AnalysisRun: ar2.Name,
		},
	}

	testCases := []struct {
		name     string
		first    v1alpha1.AnalysisStatus
		second   v1alpha1.AnalysisStatus
		expected v1alpha1.AnalysisStatus
	}{
		{
			name:     "all successful",
			first:    v1alpha1.AnalysisStatusSuccessful,
			second:   v1alpha1.AnalysisStatusSuccessful,
			expected: v1alpha1.AnalysisStatusSuccessful,
		},
		{
			name:     "failed,successful",
			first:    v1alpha1.AnalysisStatusFailed,
			second:   v1alpha1.AnalysisStatusSuccessful,
			expected: v1alpha1.AnalysisStatusFailed,
		},
		{
			name:     "successful,failed",
			first:    v1alpha1.AnalysisStatusSuccessful,
			second:   v1alpha1.AnalysisStatusFailed,
			expected: v1alpha1.AnalysisStatusFailed,
		},
		{
			name:     "running,successful",
			first:    v1alpha1.AnalysisStatusRunning,
			second:   v1alpha1.AnalysisStatusSuccessful,
			expected: v1alpha1.AnalysisStatusRunning,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			log.Info(test.name)
			ar1.Status.Status = test.first
			e.Status.AnalysisRuns[0].Status = test.first
			ar2.Status.Status = test.second
			e.Status.AnalysisRuns[1].Status = test.second
			f := newFixture(t, e, rs, ar1, ar2)
			if test.expected != v1alpha1.AnalysisStatusRunning {
				patchIdx := f.expectPatchExperimentAction(e)
				f.run(getKey(e, t))
				patchedEx := f.getPatchedExperimentAsObj(patchIdx)
				assert.Equal(t, test.expected, patchedEx.Status.Status)
			} else {
				f.run(getKey(e, t))
			}
			f.Close()
		})
	}
}

// TestFailExperimentWhenAnalysisFails verifies that an failed analysis can cause the experiment to
// prematurely fail
func TestFailExperimentWhenAnalysisFails(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil)
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{
		{
			Name:         "success-rate",
			TemplateName: "success-rate",
		},
		{
			Name:         "latency",
			TemplateName: "latency",
		},
	}
	e.Status.Status = v1alpha1.AnalysisStatusRunning
	e.Spec.Duration = pointer.Int32Ptr(300)
	e.Status.AvailableAt = secondsAgo(60)
	rs := templateToRS(e, templates[0], 1)
	ar1 := analysisTemplateToRun("success-rate", e, &v1alpha1.AnalysisTemplateSpec{})
	ar1.Name = ar1.GenerateName + "abc123"
	ar2 := analysisTemplateToRun("latency", e, &v1alpha1.AnalysisTemplateSpec{})
	ar2.Name = ar2.GenerateName + "abc123"

	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}
	e.Status.AnalysisRuns = []v1alpha1.ExperimentAnalysisRunStatus{
		{
			Name:        e.Spec.Analyses[0].Name,
			AnalysisRun: ar1.Name,
		},
		{
			Name:        e.Spec.Analyses[1].Name,
			AnalysisRun: ar2.Name,
		},
	}

	testCases := []struct {
		name     string
		first    v1alpha1.AnalysisStatus
		second   v1alpha1.AnalysisStatus
		expected v1alpha1.AnalysisStatus
	}{
		{
			name:     "all successful",
			first:    v1alpha1.AnalysisStatusSuccessful,
			second:   v1alpha1.AnalysisStatusSuccessful,
			expected: "", // empty string means patch did not update the experiment status
		},
		{
			name:     "failed,successful",
			first:    v1alpha1.AnalysisStatusFailed,
			second:   v1alpha1.AnalysisStatusSuccessful,
			expected: v1alpha1.AnalysisStatusFailed,
		},
		{
			name:     "successful,failed",
			first:    v1alpha1.AnalysisStatusSuccessful,
			second:   v1alpha1.AnalysisStatusFailed,
			expected: v1alpha1.AnalysisStatusFailed,
		},
		{
			name:     "running,successful",
			first:    v1alpha1.AnalysisStatusRunning,
			second:   v1alpha1.AnalysisStatusSuccessful,
			expected: "", // empty string means patch did not update the experiment status
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			log.Info(test.name)
			ar1.Status.Status = test.first
			e.Status.AnalysisRuns[0].Status = test.first
			ar2.Status.Status = test.second
			e.Status.AnalysisRuns[1].Status = test.second
			f := newFixture(t, e, rs, ar1, ar2)

			if test.expected == v1alpha1.AnalysisStatusFailed {
				f.expectUpdateReplicaSetAction(rs) // scale down to 0
			}
			patchIdx := f.expectPatchExperimentAction(e)
			f.run(getKey(e, t))
			patchedEx := f.getPatchedExperimentAsObj(patchIdx)
			assert.Equal(t, test.expected, patchedEx.Status.Status)

			f.Close()
		})
	}
}

// TestTerminateAnalysisRuns verifies we terminate analysis runs when experiment is terminating
func TestTerminateAnalysisRuns(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil)
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{
		{
			Name:         "success-rate",
			TemplateName: "success-rate",
		},
	}
	e.Spec.Terminate = true
	e.Status.Status = v1alpha1.AnalysisStatusRunning
	e.Status.AvailableAt = secondsAgo(60)
	rs := templateToRS(e, templates[0], 0)
	rs.Spec.Replicas = new(int32)
	ar := analysisTemplateToRun("success-rate", e, &v1alpha1.AnalysisTemplateSpec{})
	ar.Name = ar.GenerateName + "abc123"
	ar.Status = v1alpha1.AnalysisRunStatus{
		Status: v1alpha1.AnalysisStatusRunning,
	}
	e.Status.AnalysisRuns = []v1alpha1.ExperimentAnalysisRunStatus{
		{
			Name:        e.Spec.Analyses[0].Name,
			Status:      v1alpha1.AnalysisStatusRunning,
			AnalysisRun: ar.Name,
		},
	}

	f := newFixture(t, e, rs, ar)
	defer f.Close()

	arPatchIdx := f.expectPatchAnalysisRunAction(ar)
	f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))

	patchedAr := f.getPatchedAnalysisRunAsObj(arPatchIdx)
	assert.True(t, patchedAr.Spec.Terminate)
}
