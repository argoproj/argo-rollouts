package create

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	core "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestCreateRollout(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreate(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"-f", "../../../../examples/rollout-canary.yaml"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	assert.Equal(t, "rollout.argoproj.io/rollout-canary created\n", stdout)
}

func TestCreateExperiment(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreate(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"-f", "../../../../examples/experiment-with-analysis.yaml"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	assert.Equal(t, "experiment.argoproj.io/experiment-with-analysis created\n", stdout)
}

func TestCreateAnalysisTemplate(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreate(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"-f", "testdata/analysis-template.yaml"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	assert.Equal(t, "analysistemplate.argoproj.io/pass created\n", stdout)
}

func TestCreateAnalysisRun(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreate(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"-f", "../../../../test/e2e/functional/analysis-run-job.yaml"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	assert.Equal(t, "analysisrun.argoproj.io/ created\n", stdout)
}

func TestCreateAnalysisRunUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	err := cmd.Execute()
	assert.EqualError(t, err, "one of --from or --from-file must be specified")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: one of --from or --from-file must be specified\n", stderr)
}

func TestCreateAnalysisRunMultipleFromSources(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/analysis-template.yaml", "--from", "my-template"})
	err := cmd.Execute()
	assert.EqualError(t, err, "one of --from or --from-file must be specified")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: one of --from or --from-file must be specified\n", stderr)
}

func TestCreateAnalysisRunUnresolvedArg(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/analysis-template.yaml"})
	err := cmd.Execute()
	assert.EqualError(t, err, "args.foo was not resolved")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: args.foo was not resolved\n", stderr)
}

func TestCreateAnalysisRunBadArg(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/analysis-template.yaml", "-a", "bad-syntax"})
	err := cmd.Execute()
	assert.EqualError(t, err, "arguments must be in the form NAME=VALUE")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: arguments must be in the form NAME=VALUE\n", stderr)
}

func TestCreateAnalysisRunDefaultGenerateName(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/analysis-template.yaml", "-a", "foo=bar"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "analysisrun.argoproj.io/ created\n", stdout) // note this uses generate name, so name is empty
	assert.Empty(t, stderr)
}

func TestCreateAnalysisRunName(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/analysis-template.yaml", "-a", "foo=bar", "--name", "my-run"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "analysisrun.argoproj.io/my-run created\n", stdout)
	assert.Empty(t, stderr)
}

func TestCreateAnalysisRunNameNotSpecified(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()

	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/analysis-template-no-name.yaml", "-a", "foo=bar"})
	err := cmd.Execute()
	assert.EqualError(t, err, "name is invalid")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: name is invalid\n", stderr)
}

func TestCreateAnalysisRunWithInstanceID(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	fakeClient := o.DynamicClientset().(*dynamicfake.FakeDynamicClient)
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/analysis-template.yaml", "-a", "foo=bar", "--name", "my-run", "--instance-id", "test"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "analysisrun.argoproj.io/my-run created\n", stdout)
	assert.Empty(t, stderr)
	assert.Len(t, fakeClient.Actions(), 1)
	action := fakeClient.Actions()[0].(core.CreateAction)
	objMap, err := runtime.NewTestUnstructuredConverter(equality.Semantic).ToUnstructured(action.GetObject())
	assert.Nil(t, err)
	obj := unstructured.Unstructured{Object: objMap}
	assert.Equal(t, obj.GetLabels()[v1alpha1.LabelKeyControllerInstanceID], "test")
}

func TestCreateJSON(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreate(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"-f", "testdata/analysis-template.json"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	assert.Equal(t, "analysistemplate.argoproj.io/pass created\n", stdout)
}

func TestCreateAnalysisRunFromTemplateInCluster(t *testing.T) {
	var template unstructured.Unstructured
	fileBytes, err := os.ReadFile("testdata/analysis-template.yaml")
	assert.NoError(t, err)
	err = unmarshal(fileBytes, &template)
	assert.NoError(t, err)
	err = unstructured.SetNestedField(template.Object, "default", "metadata", "namespace")
	assert.NoError(t, err)

	tf, o := options.NewFakeArgoRolloutsOptions(&template)
	defer tf.Cleanup()

	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from", "pass", "-a", "foo=bar", "--name", "my-run"})
	err = cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "analysisrun.argoproj.io/my-run created\n", stdout)
	assert.Empty(t, stderr)
}

func TestCreateAnalysisRunFromTemplateNotFoundInCluster(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()

	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from", "pass", "-a", "foo=bar", "--name", "my-run"})
	err := cmd.Execute()
	assert.EqualError(t, err, "analysistemplates.argoproj.io \"pass\" not found")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: analysistemplates.argoproj.io \"pass\" not found\n", stderr)
}

func TestCreateAnalysisRunFromClusterTemplateInCluster(t *testing.T) {
	var template unstructured.Unstructured
	fileBytes, err := os.ReadFile("testdata/cluster-analysis-template.yaml")
	assert.NoError(t, err)
	err = unmarshal(fileBytes, &template)
	assert.NoError(t, err)

	tf, o := options.NewFakeArgoRolloutsOptions(&template)
	defer tf.Cleanup()

	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from", "pass", "-a", "foo=bar", "--name", "my-run", "--global"})
	err = cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "analysisrun.argoproj.io/my-run created\n", stdout)
	assert.Empty(t, stderr)
}

func TestCreateAnalysisRunFromClusterTemplateNotFoundInCluster(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()

	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from", "pass", "-a", "foo=bar", "--name", "my-run", "--global"})
	err := cmd.Execute()
	assert.EqualError(t, err, "clusteranalysistemplates.argoproj.io \"pass\" not found")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: clusteranalysistemplates.argoproj.io \"pass\" not found\n", stderr)
}

func TestCreateAnalysisRunFromClusterTemplateBadArg(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/cluster-analysis-template.yaml", "-a", "bad-syntax", "--global"})
	err := cmd.Execute()
	assert.EqualError(t, err, "arguments must be in the form NAME=VALUE")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: arguments must be in the form NAME=VALUE\n", stderr)
}

func TestCreateAnalysisFromClusterTemplateRunUnresolvedArg(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCreateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--from-file", "testdata/cluster-analysis-template.yaml", "--global"})
	err := cmd.Execute()
	assert.EqualError(t, err, "args.foo was not resolved")
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: args.foo was not resolved\n", stderr)
}
