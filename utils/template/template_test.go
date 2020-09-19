package query

import (
	"fmt"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/stretchr/testify/assert"
)

func TestResolveExperimentArgsValueInvalidTemplate(t *testing.T) {
	_, err := ResolveExperimentArgsValue("test-{{args.var", nil, nil)
	assert.Equal(t, fmt.Errorf("Cannot find end tag=\"}}\" in the template=\"test-{{args.var\" starting from \"args.var\""), err)
}

func TestResolveExperimentArgsValueInvalidDuration(t *testing.T) {
	now := metav1.Now()
	ex := &v1alpha1.Experiment{
		Spec: v1alpha1.ExperimentSpec{
			Duration: "asdf",
		},
		Status: v1alpha1.ExperimentStatus{
			AvailableAt: &now,
		},
	}
	_, err := ResolveExperimentArgsValue("test", ex, nil)
	assert.Regexp(t, `time: invalid duration (")?asdf(")?`, err)
}

func TestResolveExperimentArgsValue(t *testing.T) {
	now := metav1.Now()
	ex := &v1alpha1.Experiment{
		Spec: v1alpha1.ExperimentSpec{
			Duration: "1m",
			Templates: []v1alpha1.TemplateSpec{{
				Name: "test",
			}},
		},
		Status: v1alpha1.ExperimentStatus{
			AvailableAt: &now,
		},
	}
	rsMap := map[string]*appsv1.ReplicaSet{
		"test": {
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					v1alpha1.DefaultRolloutUniqueLabelKey: "abcd",
				},
			},
		},
	}
	argValue, err := ResolveExperimentArgsValue("{{templates.test.podTemplateHash}}", ex, rsMap)
	assert.Nil(t, err)
	assert.Equal(t, "abcd", argValue)
	argValue, err = ResolveExperimentArgsValue("{{experiment.availableAt}}", ex, rsMap)
	assert.Nil(t, err)
	assert.Equal(t, now.Format(time.RFC3339), argValue)
	argValue, err = ResolveExperimentArgsValue("{{experiment.finishedAt}}", ex, rsMap)
	assert.Nil(t, err)
	assert.Equal(t, now.Add(1*time.Minute).Format(time.RFC3339), argValue)
}

func TestResolveArgsWithNoSubstitution(t *testing.T) {
	query, err := ResolveArgs("test", nil)
	assert.Nil(t, err)
	assert.Equal(t, "test", query)
}

func TestResolveArgsRemoveWhiteSpace(t *testing.T) {
	args := []v1alpha1.Argument{{
		Name:  "var",
		Value: pointer.StringPtr("foo"),
	}}
	query, err := ResolveArgs("test-{{ args.var }}", args)
	assert.Nil(t, err)
	assert.Equal(t, "test-foo", query)
}

func TestResolveArgsWithSubstitution(t *testing.T) {
	args := []v1alpha1.Argument{{
		Name:  "var",
		Value: pointer.StringPtr("foo"),
	}}
	query, err := ResolveArgs("test-{{args.var}}", args)
	assert.Nil(t, err)
	assert.Equal(t, "test-foo", query)
}

func TestInvalidTemplate(t *testing.T) {
	_, err := ResolveArgs("test-{{args.var", nil)
	assert.Equal(t, fmt.Errorf("Cannot find end tag=\"}}\" in the template=\"test-{{args.var\" starting from \"args.var\""), err)
}

func TestMissingArgs(t *testing.T) {
	_, err := ResolveArgs("test-{{args.var}}", nil)
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Errorf("failed to resolve {{args.var}}"), err)
}

func TestResolveArgsValueNotSupplied(t *testing.T) {
	args := []v1alpha1.Argument{{Name: "test"}}
	_, err := ResolveArgs("{{args.test}}", args)
	assert.Equal(t, fmt.Errorf("argument \"test\" was not supplied"), err)
}

func TestResolveQuotedArgs(t *testing.T) {
	args := []v1alpha1.Argument{
		{
			Name:  "var",
			Value: pointer.StringPtr("double quotes\"newline\nand tab\t"),
		},
	}
	{
		query, err := ResolveQuotedArgs("test-{{args.var}}", args)
		assert.Nil(t, err)
		assert.Equal(t, "test-double quotes\\\"newline\\nand tab\\t", query)
	}
	{
		query, err := ResolveArgs("test-{{args.var}}", args)
		assert.Nil(t, err)
		assert.Equal(t, "test-double quotes\"newline\nand tab\t", query)
	}
}
