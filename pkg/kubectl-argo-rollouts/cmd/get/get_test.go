package get

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestGetRolloutUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdGet(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRolloutNotFound(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdGet(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"does-not-exist"})
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
	cmd := NewCmdGet(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, "--no-color"})
	err := cmd.Execute()
	assert.NoError(t, err)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	expectedOut := strings.TrimPrefix(`
Name:            bluegreen-demo
Namespace:       jesse-test
Status:          ‖ Paused
Strategy:        BlueGreen
Images:          argoproj/rollouts-demo:blue
                 argoproj/rollouts-demo:green
Replicas:
  Desired:       3
  Current:       6
  Updated:       3
  Ready:         6
  Available:     3

NAME                                       KIND        STATUS        INFO       AGE
⟳ bluegreen-demo                           Rollout     ‖ Paused                 7d
├───⧉ bluegreen-demo-74b948fccb (rev:11)   ReplicaSet  ✔ Healthy     preview    7d
│   ├───□ bluegreen-demo-74b948fccb-5jz59  Pod         ✔ Running     ready:1/1  7d
│   ├───□ bluegreen-demo-74b948fccb-mkhrl  Pod         ✔ Running     ready:1/1  7d
│   └───□ bluegreen-demo-74b948fccb-vvj2t  Pod         ✔ Running     ready:1/1  7d
├───⧉ bluegreen-demo-6cbccd9f99 (rev:10)   ReplicaSet  ✔ Healthy     active     7d
│   ├───□ bluegreen-demo-6cbccd9f99-gk78v  Pod         ✔ Running     ready:1/1  7d
│   ├───□ bluegreen-demo-6cbccd9f99-kxj8g  Pod         ✔ Running     ready:1/1  7d
│   └───□ bluegreen-demo-6cbccd9f99-t2d4f  Pod         ✔ Running     ready:1/1  7d
└───⧉ bluegreen-demo-746d5fddf6 (rev:8)    ReplicaSet  • ScaledDown             7d
`, "\n")
	assert.Equal(t, expectedOut, stdout)

}

func TestGetCanaryRollout(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdGet(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, "--no-color"})
	err := cmd.Execute()
	assert.NoError(t, err)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	expectedOut := strings.TrimPrefix(`
Name:            canary-demo
Namespace:       jesse-test
Status:          ✖ Degraded
Strategy:        Canary
  Step:          0/8
  SetWeight:     20
  ActualWeight:  0
Images:          argoproj/rollouts-demo:green
Replicas:
  Desired:       5
  Current:       6
  Updated:       1
  Ready:         5
  Available:     5

NAME                                    KIND        STATUS              INFO       AGE
⟳ canary-demo                           Rollout     ✖ Degraded                     7d
├───⧉ canary-demo-65fb5ffc84 (rev:31)   ReplicaSet  ◷ Progressing       canary     7d
│   └───□ canary-demo-65fb5ffc84-9wf5r  Pod         ✖ ImagePullBackOff  ready:0/1  7d
├───⧉ canary-demo-877894d5b (rev:30)    ReplicaSet  ✔ Healthy           stable     7d
│   ├───□ canary-demo-877894d5b-6jfpt   Pod         ✔ Running           ready:1/1  7d
│   ├───□ canary-demo-877894d5b-7jmqw   Pod         ✔ Running           ready:1/1  7d
│   ├───□ canary-demo-877894d5b-j8g2b   Pod         ✔ Running           ready:1/1  7d
│   ├───□ canary-demo-877894d5b-jw5qm   Pod         ✔ Running           ready:1/1  7d
│   └───□ canary-demo-877894d5b-kh7x4   Pod         ✔ Running           ready:1/1  7d
└───⧉ canary-demo-859c99b45c (rev:29)   ReplicaSet  • ScaledDown                   7d
`, "\n")
	assert.Equal(t, expectedOut, stdout)
}
