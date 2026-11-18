package analysis

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
)

func TestAnalysisCreationCallbackRunsOnlyForNewObjects(t *testing.T) {
	for _, phase := range []v1alpha1.AnalysisPhase{"", v1alpha1.AnalysisPhaseRunning, v1alpha1.AnalysisPhaseSuccessful} {
		t.Run(string(phase), func(t *testing.T) {
			run := v1alpha1.AnalysisRun{ObjectMeta: metav1.ObjectMeta{
				Name: "gate", Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{Name: "checkout", UID: "owner", Controller: ptr.To(true)}},
			}}
			var existing []runtime.Object
			if phase != "" {
				old := run.DeepCopy()
				old.Status.Phase = phase
				existing = append(existing, old)
			}
			client := fake.NewSimpleClientset(existing...).ArgoprojV1alpha1().AnalysisRuns(run.Namespace)
			var created []*v1alpha1.AnalysisRun
			result, err := CreateWithCollisionCounterWithCallback(log.NewEntry(log.New()), client, run, func(actual *v1alpha1.AnalysisRun) {
				created = append(created, actual)
			})
			require.NoError(t, err)
			if phase == v1alpha1.AnalysisPhaseRunning {
				assert.Empty(t, created)
			} else {
				require.Len(t, created, 1)
				assert.Same(t, result, created[0])
				if phase == v1alpha1.AnalysisPhaseSuccessful {
					assert.Equal(t, "gate.1", result.Name)
				}
			}
		})
	}
}
