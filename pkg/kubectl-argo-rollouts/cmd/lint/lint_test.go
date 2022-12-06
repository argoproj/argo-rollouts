package lint

import (
	"bytes"
	"testing"

	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
	"github.com/stretchr/testify/assert"
)

func TestLintValidRollout(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()

	cmd := NewCmdLint(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE

	tests := []string{
		"testdata/valid.yml",
		"testdata/valid.json",
		"testdata/valid-workload-ref.yaml",
		"testdata/valid-with-another-empty-object.yml",
		"testdata/valid-istio-v1alpha3.yml",
		"testdata/valid-istio-v1beta1.yml",
		"testdata/valid-blue-green.yml",
		"testdata/valid-ingress-smi.yml",
		"testdata/valid-ingress-smi-multi.yml",
		"testdata/valid-alb-canary.yml",
		"testdata/valid-nginx-canary.yml",
		"testdata/valid-nginx-basic-canary.yml",
		"testdata/valid-istio-v1beta1-mulitiple-virtualsvcs.yml",
		"testdata/valid-nginx-smi-with-vsvc.yaml",
	}

	for _, filename := range tests {
		t.Run(filename, func(t *testing.T) {
			cmd.SetArgs([]string{"-f", filename})
			err := cmd.Execute()
			assert.NoError(t, err)

			stdout := o.Out.(*bytes.Buffer).String()
			assert.Empty(t, stdout)
		})
	}
}

func TestLintInvalidRollout(t *testing.T) {
	var runCmd func(string, string)

	tests := []struct {
		filename string
		errmsg   string
	}{
		{
			"testdata/invalid.yml",
			"Error: spec.strategy.maxSurge: Invalid value: intstr.IntOrString{Type:0, IntVal:0, StrVal:\"\"}: MaxSurge and MaxUnavailable both can not be zero\n",
		},
		{
			"testdata/invalid-empty-rollout-vsvc.yml",
			"Error: spec.selector: Required value: Rollout has missing field '.spec.selector'\n",
		},
		{
			"testdata/invalid.json",
			"Error: spec.strategy.maxSurge: Invalid value: intstr.IntOrString{Type:0, IntVal:0, StrVal:\"\"}: MaxSurge and MaxUnavailable both can not be zero\n",
		},
		{
			"testdata/invalid-multiple-docs.yml",
			"Error: spec.strategy.maxSurge: Invalid value: intstr.IntOrString{Type:0, IntVal:0, StrVal:\"\"}: MaxSurge and MaxUnavailable both can not be zero\n",
		},
		{
			"testdata/invalid-unknown-field.yml",
			"Error: error unmarshaling JSON: while decoding JSON: json: unknown field \"unknown-strategy\"\n",
		},
		{
			"testdata/invalid-service-labels.yml",
			"Error: spec.strategy.canary.canaryService: Invalid value: \"istio-host-split-canary\": Service \"istio-host-split-canary\" has unmatch label \"app\" in rollout\n",
		},
		{
			"testdata/invalid-ping-pong.yml",
			"Error: spec.strategy.canary.pingPong.pingService: Invalid value: \"ping-service\": Service \"ping-service\" has unmatch label \"app\" in rollout\n",
		},
		{
			"testdata/invalid-ingress-smi-multi.yml",
			"Error: spec.strategy.canary.canaryService: Invalid value: \"rollout-smi-experiment-canary\": Service \"rollout-smi-experiment-canary\" has unmatch label \"app\" in rollout\n",
		},
		{
			filename: "testdata/invalid-nginx-canary.yml",
			errmsg:   "Error: spec.strategy.steps[1].experiment.templates[0].weight: Invalid value: 20: Experiment template weight is only available for TrafficRouting with SMI, ALB, and Istio at this time\n",
		},
	}

	runCmd = func(filename string, errmsg string) {
		t.Run(filename, func(t *testing.T) {
			tf, o := options.NewFakeArgoRolloutsOptions()
			defer tf.Cleanup()

			cmd := NewCmdLint(o)
			cmd.PersistentPreRunE = o.PersistentPreRunE
			cmd.SetArgs([]string{"-f", filename})
			err := cmd.Execute()
			assert.Error(t, err)

			stdout := o.Out.(*bytes.Buffer).String()
			stderr := o.ErrOut.(*bytes.Buffer).String()
			assert.Empty(t, stdout)
			assert.Equal(t, errmsg, stderr)
		})
	}

	for _, t := range tests {
		runCmd(t.filename, t.errmsg)
	}
}
