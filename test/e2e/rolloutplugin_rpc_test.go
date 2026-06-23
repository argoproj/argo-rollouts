//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

const E2EResourcePluginName = "resource/e2e-test"

type RolloutPluginRpcSuite struct {
	fixtures.E2ESuite
}

func TestRolloutPluginRpcSuite(t *testing.T) {
	suite.Run(t, new(RolloutPluginRpcSuite))
}

func (s *RolloutPluginRpcSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	if !isResourcePluginConfigured(&s.Common, s.GetControllerConfig()) {
		s.T().SkipNow()
	}
}

// isResourcePluginConfigured checks the controller ConfigMap for the resource plugin entry.
func isResourcePluginConfigured(c *fixtures.Common, config *corev1.ConfigMap) bool {
	if config == nil {
		return false
	}

	var resourcePlugins []types.PluginItem
	if err := yaml.Unmarshal([]byte(config.Data["rolloutPlugins"]), &resourcePlugins); err != nil {
		c.CheckError(err)
	}

	for _, p := range resourcePlugins {
		if p.Name == E2EResourcePluginName {
			return true
		}
	}
	return false
}

// TestRolloutPluginRpcBasicCanary tests a basic canary rollout using an external RPC resource plugin.
func (s *RolloutPluginRpcSuite) TestRolloutPluginRpcBasicCanary() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-rpc-basic
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-rpc-basic
  plugin:
    name: resource/e2e-test
  strategy:
    canary:
      steps:
      - setWeight: 50
      - pause: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-rpc-basic
spec:
  serviceName: rp-rpc-basic
  replicas: 2
  selector:
    matchLabels:
      app: rp-rpc-basic
  template:
    metadata:
      labels:
        app: rp-rpc-basic
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Then().
		ExpectRolloutPluginStatus(rov1.RolloutPluginPhasePaused).
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhasePaused, rp.Status.Phase)
			assert.NotNil(s.T(), rp.Status.CurrentStepIndex)
			assert.Equal(s.T(), int32(1), *rp.Status.CurrentStepIndex)
		}).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.Equal(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision)
		})
}
