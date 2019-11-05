package get

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func assertStdout(t *testing.T, expectedOut string, o genericclioptions.IOStreams) {
	t.Helper()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)

	expectedOut = stripTrailingWhitespace(expectedOut)
	stdout := stripTrailingWhitespace(o.Out.(*bytes.Buffer).String())
	if !assert.Equal(t, expectedOut, stdout) {
		fmt.Println("\n" + stdout)
	}
}

// stripTrailingWhitespace is a helper to strip trailing spaces from every line of the output
func stripTrailingWhitespace(s string) string {
	var newLines []string
	for _, line := range strings.Split(s, "\n") {
		newLines = append(newLines, strings.TrimRight(line, " "))
	}
	return strings.Join(newLines, "\n")
}

func TestGetUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdGet(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestGetRolloutUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdGetRollout(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestGetExperimentUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdGetExperiment(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRolloutNotFound(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdGetRollout(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"does-not-exist"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollout.argoproj.io \"does-not-exist\" not found\n", stderr)
}

func TestWatchRolloutNotFound(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdGetRollout(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"does-not-exist", "-w"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollout.argoproj.io \"does-not-exist\" not found\n", stderr)
}

func TestGetBlueGreenRollout(t *testing.T) {
	rolloutObjs := testdata.NewBlueGreenRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdGetRollout(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, "--no-color"})
	err := cmd.Execute()
	assert.NoError(t, err)

	expectedOut := strings.TrimPrefix(`
Name:            bluegreen-demo
Namespace:       jesse-test
Status:          ॥ Paused
Strategy:        BlueGreen
Images:          argoproj/rollouts-demo:blue (active)
                 argoproj/rollouts-demo:green (preview)
Replicas:
  Desired:       3
  Current:       6
  Updated:       3
  Ready:         6
  Available:     3

NAME                                        KIND        STATUS        AGE  INFO
⟳ bluegreen-demo                            Rollout     ॥ Paused      7d
├──# revision:11
│  └──⧉ bluegreen-demo-74b948fccb           ReplicaSet  ✔ Healthy     7d   preview
│     ├──□ bluegreen-demo-74b948fccb-5jz59  Pod         ✔ Running     7d   ready:1/1
│     ├──□ bluegreen-demo-74b948fccb-mkhrl  Pod         ✔ Running     7d   ready:1/1
│     └──□ bluegreen-demo-74b948fccb-vvj2t  Pod         ✔ Running     7d   ready:1/1
├──# revision:10
│  └──⧉ bluegreen-demo-6cbccd9f99           ReplicaSet  ✔ Healthy     7d   active
│     ├──□ bluegreen-demo-6cbccd9f99-gk78v  Pod         ✔ Running     7d   ready:1/1
│     ├──□ bluegreen-demo-6cbccd9f99-kxj8g  Pod         ✔ Running     7d   ready:1/1
│     └──□ bluegreen-demo-6cbccd9f99-t2d4f  Pod         ✔ Running     7d   ready:1/1
└──# revision:8
   └──⧉ bluegreen-demo-746d5fddf6           ReplicaSet  • ScaledDown  7d
`, "\n")
	assertStdout(t, expectedOut, o.IOStreams)
}

func TestGetCanaryRollout(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdGetRollout(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, "--no-color"})
	err := cmd.Execute()
	assert.NoError(t, err)

	expectedOut := strings.TrimPrefix(`
Name:            canary-demo
Namespace:       jesse-test
Status:          ✖ Degraded
Strategy:        Canary
  Step:          0/8
  SetWeight:     20
  ActualWeight:  0
Images:          argoproj/rollouts-demo:does-not-exist (canary)
                 argoproj/rollouts-demo:green (stable)
Replicas:
  Desired:       5
  Current:       6
  Updated:       1
  Ready:         5
  Available:     5

NAME                                     KIND        STATUS              AGE  INFO
⟳ canary-demo                            Rollout     ✖ Degraded          7d
├──# revision:31
│  └──⧉ canary-demo-65fb5ffc84           ReplicaSet  ◌ Progressing       7d   canary
│     └──□ canary-demo-65fb5ffc84-9wf5r  Pod         ⚠ ImagePullBackOff  7d   ready:0/1
├──# revision:30
│  └──⧉ canary-demo-877894d5b            ReplicaSet  ✔ Healthy           7d   stable
│     ├──□ canary-demo-877894d5b-6jfpt   Pod         ✔ Running           7d   ready:1/1
│     ├──□ canary-demo-877894d5b-7jmqw   Pod         ✔ Running           7d   ready:1/1
│     ├──□ canary-demo-877894d5b-j8g2b   Pod         ✔ Running           7d   ready:1/1
│     ├──□ canary-demo-877894d5b-jw5qm   Pod         ✔ Running           7d   ready:1/1
│     └──□ canary-demo-877894d5b-kh7x4   Pod         ✔ Running           7d   ready:1/1
└──# revision:29
   └──⧉ canary-demo-859c99b45c           ReplicaSet  • ScaledDown        7d
`, "\n")
	assertStdout(t, expectedOut, o.IOStreams)
}

func TestExperimentRollout(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdGetRollout(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, "--no-color"})
	err := cmd.Execute()
	assert.NoError(t, err)

	expectedOut := strings.TrimPrefix(`
Name:            rollout-experiment-analysis
Namespace:       jesse-test
Status:          ✖ Degraded
Strategy:        Canary
  Step:          1/2
  SetWeight:     25
  ActualWeight:  25
Images:          argoproj/rollouts-demo:blue (stable)
                 argoproj/rollouts-demo:yellow (canary)
Replicas:
  Desired:       4
  Current:       4
  Updated:       1
  Ready:         4
  Available:     4

NAME                                                                           KIND         STATUS          AGE  INFO
⟳ rollout-experiment-analysis                                                  Rollout      ✖ Degraded      7d
├──# revision:2
│  ├──⧉ rollout-experiment-analysis-6f646bf7b7                                 ReplicaSet   ✔ Healthy       7d   canary
│  │  └──□ rollout-experiment-analysis-6f646bf7b7-wn5w8                        Pod          ✔ Running       7d   ready:1/1
│  ├──Σ rollout-experiment-analysis-6f646bf7b7-1-vcv27                         Experiment   ◌ Running       7d
│  │  ├──⧉ rollout-experiment-analysis-6f646bf7b7-1-vcv27-baseline-7d768b8b5f  ReplicaSet   ✔ Healthy       7d
│  │  │  └──□ rollout-experiment-analysis-6f646bf7b7-1-vcv27-baseline-7dczdst  Pod          ✔ Running       7d   ready:1/1
│  │  └──⧉ rollout-experiment-analysis-6f646bf7b7-1-vcv27-canary-7699dcf5d     ReplicaSet   ✔ Healthy       7d
│  │     └──□ rollout-experiment-analysis-6f646bf7b7-1-vcv27-canary-7699vgr24  Pod          ✔ Running       7d   ready:1/1
│  └──α rollout-experiment-analysis-random-fail-6f646bf7b7-skqcr               AnalysisRun  ? Inconclusive  7d   ✔ 4,✖ 4,? 1,⚠ 1
└──# revision:1
   └──⧉ rollout-experiment-analysis-f6db98dff                                  ReplicaSet   ✔ Healthy       7d   stable
      ├──□ rollout-experiment-analysis-f6db98dff-8dmnz                         Pod          ✔ Running       7d   ready:1/1
      ├──□ rollout-experiment-analysis-f6db98dff-bb6v6                         Pod          ✔ Running       7d   ready:1/1
      └──□ rollout-experiment-analysis-f6db98dff-bq55x                         Pod          ✔ Running       7d   ready:1/1
`, "\n")
	assertStdout(t, expectedOut, o.IOStreams)
}

func TestGetExperiment(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdGetExperiment(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Experiments[0].Name, "--no-color"})
	err := cmd.Execute()
	assert.NoError(t, err)

	expectedOut := strings.TrimPrefix(`
Name:            rollout-experiment-analysis-6f646bf7b7-1-vcv27
Namespace:       jesse-test
Status:          ◌ Running
Images:          argoproj/rollouts-demo:blue
                 argoproj/rollouts-demo:yellow

NAME                                                                     KIND        STATUS     AGE  INFO
Σ rollout-experiment-analysis-6f646bf7b7-1-vcv27                         Experiment  ◌ Running  7d
├──⧉ rollout-experiment-analysis-6f646bf7b7-1-vcv27-baseline-7d768b8b5f  ReplicaSet  ✔ Healthy  7d
│  └──□ rollout-experiment-analysis-6f646bf7b7-1-vcv27-baseline-7dczdst  Pod         ✔ Running  7d   ready:1/1
└──⧉ rollout-experiment-analysis-6f646bf7b7-1-vcv27-canary-7699dcf5d     ReplicaSet  ✔ Healthy  7d
   └──□ rollout-experiment-analysis-6f646bf7b7-1-vcv27-canary-7699vgr24  Pod         ✔ Running  7d   ready:1/1
`, "\n")
	assertStdout(t, expectedOut, o.IOStreams)
}
