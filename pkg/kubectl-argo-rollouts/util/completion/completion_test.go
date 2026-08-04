package completion

import (
	"sort"
	"testing"

	"github.com/spf13/cobra"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	fakeoptions "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestRolloutNameCompletionFuncNoArgs(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	_, cmd, o := prepareCompletionTest(rolloutObjs)
	compFunc := RolloutNameCompletionFunc(o)

	comps, directive := compFunc(cmd, []string{}, "")
	checkCompletion(t, comps, []string{"canary-demo", "canary-demo-pingpong", "canary-demo-weights", "canary-demo-weights-na", "canary-demo-workloadRef", "canary-demo-workloadRef-deploy"}, directive, cobra.ShellCompDirectiveNoFileComp)

	comps, directive = compFunc(cmd, []string{}, "cana")
	checkCompletion(t, comps, []string{"canary-demo", "canary-demo-pingpong", "canary-demo-weights", "canary-demo-weights-na", "canary-demo-workloadRef", "canary-demo-workloadRef-deploy"}, directive, cobra.ShellCompDirectiveNoFileComp)

	comps, directive = compFunc(cmd, []string{}, "canary-demo")
	checkCompletion(t, comps, []string{"canary-demo", "canary-demo-pingpong", "canary-demo-weights", "canary-demo-weights-na", "canary-demo-workloadRef", "canary-demo-workloadRef-deploy"}, directive, cobra.ShellCompDirectiveNoFileComp)

	comps, directive = compFunc(cmd, []string{}, "canary-demo-p")
	checkCompletion(t, comps, []string{"canary-demo-pingpong"}, directive, cobra.ShellCompDirectiveNoFileComp)

	comps, directive = compFunc(cmd, []string{}, "seagull")
	checkCompletion(t, comps, []string{}, directive, cobra.ShellCompDirectiveNoFileComp)
}

func TestRolloutNameCompletionFuncOneArg(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	_, cmd, o := prepareCompletionTest(rolloutObjs)
	compFunc := RolloutNameCompletionFunc(o)

	comps, directive := compFunc(cmd, []string{"canary"}, "")
	checkCompletion(t, comps, []string{}, directive, cobra.ShellCompDirectiveNoFileComp)
}

func TestExperimentNameCompletionFuncNoArgs(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()
	_, cmd, o := prepareCompletionTest(rolloutObjs)
	compFunc := ExperimentNameCompletionFunc(o)

	comps, directive := compFunc(cmd, []string{}, "")
	checkCompletion(t, comps, []string{"rollout-experiment-analysis-6f646bf7b7-1-vcv27"}, directive, cobra.ShellCompDirectiveNoFileComp)

	comps, directive = compFunc(cmd, []string{}, "roll")
	checkCompletion(t, comps, []string{"rollout-experiment-analysis-6f646bf7b7-1-vcv27"}, directive, cobra.ShellCompDirectiveNoFileComp)

	comps, directive = compFunc(cmd, []string{}, "seagull")
	checkCompletion(t, comps, []string{}, directive, cobra.ShellCompDirectiveNoFileComp)
}

func TestExperimentNameCompletionFuncOneArg(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()
	_, cmd, o := prepareCompletionTest(rolloutObjs)
	compFunc := ExperimentNameCompletionFunc(o)

	comps, directive := compFunc(cmd, []string{"canary"}, "")
	checkCompletion(t, comps, []string{}, directive, cobra.ShellCompDirectiveNoFileComp)
}

func TestAnalysisRunNameCompletionFuncNoArgs(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisJobRollout()
	_, cmd, o := prepareCompletionTest(rolloutObjs)
	compFunc := AnalysisRunNameCompletionFunc(o)

	comps, directive := compFunc(cmd, []string{}, "")
	checkCompletion(t, comps, []string{"canary-demo-645d5dbc4c-2-0-stress-test"}, directive, cobra.ShellCompDirectiveNoFileComp)

	comps, directive = compFunc(cmd, []string{}, "canary")
	checkCompletion(t, comps, []string{"canary-demo-645d5dbc4c-2-0-stress-test"}, directive, cobra.ShellCompDirectiveNoFileComp)

	comps, directive = compFunc(cmd, []string{}, "seagull")
	checkCompletion(t, comps, []string{}, directive, cobra.ShellCompDirectiveNoFileComp)
}

func TestAnalysisRunNameCompletionFuncOneArg(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisJobRollout()
	_, cmd, o := prepareCompletionTest(rolloutObjs)
	compFunc := AnalysisRunNameCompletionFunc(o)

	comps, directive := compFunc(cmd, []string{"canary"}, "")
	checkCompletion(t, comps, []string{}, directive, cobra.ShellCompDirectiveNoFileComp)

}

func prepareCompletionTest(r *testdata.RolloutObjects) (*cmdtesting.TestFactory, *cobra.Command, *options.ArgoRolloutsOptions) {
	tf, o := fakeoptions.NewFakeArgoRolloutsOptions(r.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(r.Rollouts[0].Namespace)
	defer tf.Cleanup()

	cmd := &cobra.Command{
		Short:             "Fake cmd for unit-test",
		PersistentPreRunE: o.PersistentPreRunE,
	}

	return tf, cmd, o
}

func checkCompletion(t *testing.T, comps, expectedComps []string, directive, expectedDirective cobra.ShellCompDirective) {
	if e, d := expectedDirective, directive; e != d {
		t.Errorf("expected directive\n%v\nbut got\n%v", e, d)
	}

	sort.Strings(comps)
	sort.Strings(expectedComps)

	if len(expectedComps) != len(comps) {
		t.Fatalf("expected completions\n%v\nbut got\n%v", expectedComps, comps)
	}

	for i := range comps {
		if expectedComps[i] != comps[i] {
			t.Errorf("expected completions\n%v\nbut got\n%v", expectedComps, comps)
			break
		}
	}
}
