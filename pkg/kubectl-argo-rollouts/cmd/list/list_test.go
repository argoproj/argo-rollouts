package list

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func newCanaryRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "can-guestbook",
			Namespace: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(5),
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							SetWeight: pointer.Int32Ptr(10),
						},
						{
							Pause: &v1alpha1.RolloutPause{
								Duration: v1alpha1.DurationFromInt(60),
							},
						},
						{
							SetWeight: pointer.Int32Ptr(20),
						},
					},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex:  pointer.Int32Ptr(1),
			Replicas:          4,
			ReadyReplicas:     1,
			UpdatedReplicas:   3,
			AvailableReplicas: 2,
		},
	}
}

func newBlueGreenRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bg-guestbook",
			Namespace: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(5),
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex:  pointer.Int32Ptr(1),
			Replicas:          4,
			ReadyReplicas:     1,
			UpdatedReplicas:   3,
			AvailableReplicas: 2,
		},
	}
}

func TestListCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdList(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:\n  list <rollout|experiment> [flags]\n  list [command]")
}

func TestListRolloutsCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdListRollouts(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"-h"})
	usage := cmd.UsageString()
	assert.Contains(t, usage, "Usage:\n  rollouts [flags]")
	assert.Contains(t, usage, "Aliases:\n  rollouts, ro, rollout")
}

func TestListExperimentsCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdListExperiments(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	usage := cmd.UsageString()
	assert.Contains(t, usage, "Usage:\n  experiments [flags]")
	assert.Contains(t, usage, "Aliases:\n  experiments, exp, experiment")
}

func TestListRolloutsNoResources(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdListRollouts(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "No resources found.\n", stderr)
}

func TestListCanaryRollout(t *testing.T) {
	ro := newCanaryRollout()
	tf, o := options.NewFakeArgoRolloutsOptions(ro)
	o.RESTClientGetter = tf.WithNamespace("test")
	defer tf.Cleanup()
	cmd := NewCmdListRollouts(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	expectedOut := strings.TrimPrefix(`
NAME           STRATEGY   STATUS        STEP  SET-WEIGHT  READY  DESIRED  UP-TO-DATE  AVAILABLE
can-guestbook  Canary     Progressing   1/3   10          1/4    5        3           2        
`, "\n")
	assert.Equal(t, expectedOut, stdout)
}

func TestListBlueGreenResource(t *testing.T) {
	ro := newBlueGreenRollout()
	tf, o := options.NewFakeArgoRolloutsOptions(ro)
	o.RESTClientGetter = tf.WithNamespace("test")
	defer tf.Cleanup()
	cmd := NewCmdListRollouts(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	expectedOut := strings.TrimPrefix(`
NAME          STRATEGY   STATUS        STEP  SET-WEIGHT  READY  DESIRED  UP-TO-DATE  AVAILABLE
bg-guestbook  BlueGreen  Progressing   -     -           1/4    5        3           2        
`, "\n")
	assert.Equal(t, expectedOut, stdout)
}

func TestListNamespaceAndTimestamp(t *testing.T) {
	ro := newCanaryRollout()
	tf, o := options.NewFakeArgoRolloutsOptions(ro)
	o.RESTClientGetter = tf.WithNamespace("test")
	defer tf.Cleanup()
	cmd := NewCmdListRollouts(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--all-namespaces", "--timestamps"})

	err := cmd.Execute()

	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "TIMESTAMP             NAMESPACE  NAME           STRATEGY   STATUS        STEP  SET-WEIGHT  READY  DESIRED  UP-TO-DATE  AVAILABLE")
	assert.Contains(t, stdout, "test       can-guestbook  Canary     Progressing   1/3   10          1/4    5        3           2")
}

func TestListWithWatch(t *testing.T) {
	can1 := newCanaryRollout()
	bg := newBlueGreenRollout()
	can1copy := can1.DeepCopy()
	can2 := newCanaryRollout()
	can2.Status.AvailableReplicas = 3

	tf, o := options.NewFakeArgoRolloutsOptions(can1, bg)
	o.RESTClientGetter = tf.WithNamespace("test")
	defer tf.Cleanup()
	cmd := NewCmdListRollouts(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE

	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.WatchReactionChain = nil
	watcher := watch.NewFakeWithChanSize(10, false)

	watcher.Add(can1)
	watcher.Add(bg)
	watcher.Add(can1copy)
	watcher.Add(can2)
	watcher.Stop()
	callCount := 0
	fakeClient.AddWatchReactor("*", func(action kubetesting.Action) (handled bool, ret watch.Interface, err error) {
		if callCount > 0 {
			return true, nil, errors.New("intentional error")
		}
		callCount++
		return true, watcher, nil
	})

	cmd.SetArgs([]string{"--watch"})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Equal(t, "intentional error", err.Error())

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()

	expectedOut := strings.TrimPrefix(`
NAME           STRATEGY   STATUS        STEP  SET-WEIGHT  READY  DESIRED  UP-TO-DATE  AVAILABLE
bg-guestbook   BlueGreen  Progressing   -     -           1/4    5        3           2        
can-guestbook  Canary     Progressing   1/3   10          1/4    5        3           2        
can-guestbook  Canary     Progressing   1/3   10          1/4    5        3           3        
`, "\n")
	assert.Equal(t, expectedOut, stdout)

	assert.Contains(t, stderr, "intentional error")
}

func newExperiment() *v1alpha1.Experiment {
	aWeekAgo := metav1.NewTime(time.Now().Add(-7 * 24 * time.Hour).Truncate(time.Second))
	anHourAgo := metav1.NewTime(time.Now().Add(-1 * time.Hour).Truncate(time.Second))
	return &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-experiment",
			Namespace:         "test",
			CreationTimestamp: aWeekAgo,
		},
		Spec: v1alpha1.ExperimentSpec{
			Duration: "3h",
		},
		Status: v1alpha1.ExperimentStatus{
			Phase:       v1alpha1.AnalysisPhaseRunning,
			AvailableAt: &anHourAgo,
		},
	}
}

func TestListExperimentsNoResources(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdListExperiments(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "No resources found.\n", stderr)
}

func TestListExperiments(t *testing.T) {
	exp1 := newExperiment()
	exp2 := newExperiment()
	exp2.Name = "my-experiment2"
	exp2.Namespace = "my-other-namespace"
	exp2.Spec.Duration = ""
	tf, o := options.NewFakeArgoRolloutsOptions(exp1, exp2)
	o.RESTClientGetter = tf.WithNamespace("test")
	defer tf.Cleanup()
	cmd := NewCmdListExperiments(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--all-namespaces"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
	expectedOut := strings.TrimPrefix(`
NAMESPACE           NAME            STATUS   DURATION  REMAINING  AGE
my-other-namespace  my-experiment2  Running  -         -          7d 
test                my-experiment   Running  3h        119m       7d 
`, "\n")
	assert.Equal(t, expectedOut, stdout)
}
