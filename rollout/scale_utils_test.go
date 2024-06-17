package rollout

import (
	"context"
	"fmt"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type DeploymentActions interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.Deployment, error)
	Update(ctx context.Context, deployment *appsv1.Deployment, opts metav1.UpdateOptions) (*appsv1.Deployment, error)
}

type KubeClientInterface interface {
	Deployments(namespace string) DeploymentActions
	AppsV1() AppV1Interface
}

type AppV1Interface interface {
	Deployments(namespace string) DeploymentActions
}

type mockDeploymentInterface struct {
	deployment *appsv1.Deployment
}

func (m *mockDeploymentInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.Deployment, error) {
	return m.deployment, nil
}

func (m *mockDeploymentInterface) Update(ctx context.Context, deployment *appsv1.Deployment, opts metav1.UpdateOptions) (*appsv1.Deployment, error) {
	m.deployment = deployment
	return deployment, nil
}

type testKubeClient struct {
	mockDeployment DeploymentActions
}

func (t *testKubeClient) AppsV1() AppV1Interface {
	return t
}

func (t *testKubeClient) Deployments(namespace string) DeploymentActions {
	return t.mockDeployment
}

type testRolloutContext struct {
	*rolloutContext
	kubeClient KubeClientInterface
}

func createScaleDownRolloutContext(scaleDownMode string, deploymentReplicas int32, deploymentExists bool, updateError error) *testRolloutContext {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout-test",
			Namespace: "default",
			Annotations: map[string]string{
				"rollout.argoproj.io/revision": "1",
			},
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:      "workload-test",
				ScaleDown: scaleDownMode,
			},
		},
		Status: v1alpha1.RolloutStatus{
			Phase: v1alpha1.RolloutPhaseHealthy,
		},
	}

	fakeDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workload-test",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &deploymentReplicas,
		},
	}

	var k8sfakeClient *k8sfake.Clientset
	if deploymentExists {
		k8sfakeClient = k8sfake.NewSimpleClientset(fakeDeployment)
	} else {
		k8sfakeClient = k8sfake.NewSimpleClientset()
	}

	if updateError != nil {
		k8sfakeClient.PrependReactor("update", "deployments", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, updateError
		})
	}

	mockDeploy := &mockDeploymentInterface{deployment: fakeDeployment}
	testClient := &testKubeClient{mockDeployment: mockDeploy}

	ctx := &testRolloutContext{
		rolloutContext: &rolloutContext{
			rollout:      ro,
			pauseContext: &pauseContext{},
			reconcilerBase: reconcilerBase{
				argoprojclientset: &fake.Clientset{},
				kubeclientset:     k8sfakeClient,
				recorder:          record.NewFakeEventRecorder(),
			},
		},
		kubeClient: testClient,
	}
	ctx.log = logutil.WithRollout(ctx.rollout)

	return ctx
}

func TestScaleDeployment(t *testing.T) {
	tests := []struct {
		name             string
		scaleToZero      bool
		targetScale      *int32
		expectedCount    int32
		deploymentExists bool
		updateError      error
	}{
		{
			name:             "Scale down to zero",
			targetScale:      int32Ptr(0),
			expectedCount:    0,
			deploymentExists: true,
		},
		{
			name:             "Scale down to a negative value",
			targetScale:      int32Ptr(-1),
			expectedCount:    0,
			deploymentExists: true,
		},
		{
			name:             "Deployment is already scaled",
			targetScale:      int32Ptr(5),
			expectedCount:    5,
			deploymentExists: true,
		},
		{
			name:             "Error fetching deployment",
			targetScale:      int32Ptr(0),
			deploymentExists: false,
		},
		{
			name:             "Error updating deployment",
			scaleToZero:      false,
			targetScale:      int32Ptr(0),
			deploymentExists: true,
			updateError:      fmt.Errorf("fake update error"),
		},
	}

	for _, test := range tests {
		ctx := createScaleDownRolloutContext(v1alpha1.ScaleDownOnSuccess, 5, test.deploymentExists, test.updateError)
		err := ctx.scaleDeployment(test.targetScale)

		if !test.deploymentExists || test.updateError != nil {
			assert.NotNil(t, err)
			continue
		}
		assert.Nil(t, err)
		k8sfakeClient := ctx.kubeclientset.(*k8sfake.Clientset)
		updatedDeployment, err := k8sfakeClient.AppsV1().Deployments("default").Get(context.TODO(), "workload-test", metav1.GetOptions{})
		assert.Nil(t, err)
		assert.Equal(t, *updatedDeployment.Spec.Replicas, test.expectedCount)
	}
}
