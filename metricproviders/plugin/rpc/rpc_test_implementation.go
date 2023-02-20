package rpc

import (
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type testRpcPlugin struct{}

func (g *testRpcPlugin) InitPlugin() types.RpcError {
	return types.RpcError{}
}

func (g *testRpcPlugin) Run(anaysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	finishTime := v1.Time{Time: startTime.Add(10 * time.Second)}
	newMeasurement := v1alpha1.Measurement{
		Phase:      "TestCompleted",
		Message:    "Test run completed",
		StartedAt:  &startTime,
		FinishedAt: &finishTime,
		Value:      "",
		Metadata:   nil,
		ResumeAt:   nil,
	}
	if anaysisRun == nil {
		return metricutil.MarkMeasurementError(newMeasurement, fmt.Errorf("analysisRun is nil"))
	}
	return newMeasurement
}

func (g *testRpcPlugin) Resume(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return measurement
}

func (g *testRpcPlugin) Terminate(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return measurement
}

func (g *testRpcPlugin) GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) types.RpcError {
	return types.RpcError{ErrorString: "not-implemented"}
}

func (g *testRpcPlugin) Type() string {
	return "TestRPCPlugin"
}

func (g *testRpcPlugin) GetMetadata(metric v1alpha1.Metric) map[string]string {
	metricsMetadata := make(map[string]string)
	metricsMetadata["metricName"] = metric.Name
	return metricsMetadata
}
