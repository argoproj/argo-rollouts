package kayenta

import (
	//"fmt"
	//"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"time"

	"argo-rollouts/pkg/apis/rollouts/v1alpha1"
	//"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	//"github.com/argoproj/argo-rollouts/utils/evaluate"

	//metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	//templateutil "github.com/argoproj/argo-rollouts/utils/template"
)

const (
	//ProviderType indicates the provider is kayenta
	ProviderType = "Kayenta"
)

// Provider contains all the required components to run a prometheus query
type Provider struct {
	logCtx log.Entry
	client http.Client
}

// Type incidates provider is a prometheus provider
func (p *Provider) Type() string {
	return ProviderType
}

// Run queries prometheus for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	//How do I guarantee timeout with Kayenta?  Ctx??

	//Create Json
	//Make HTTP Call
	//store canaryJobId in measurement.metadata
	//set resumeAt
	//return no errors





	newMeasurement.Value = "" //Job Id

	newMeasurement.Phase = v1alpha1.AnalysisPhaseRunning
	finishedTime := metav1.Now()
	newMeasurement.FinishedAt = &finishedTime
	return newMeasurement
}

// Resume should not be used the prometheus provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	// query kayenta with job id from metadata
	// if not finishes
	// set resume at and return early
	// get score from kayenta response
	// Compare score to success and failure conditions (evaluateResult method)
	// set measurement value, finsihed time, status (healthy, inconclusive, or failure)

	return measurement
}

// Terminate should not be used the prometheus provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("kayenta provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the prometheus provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}


func NewKayentaProvider(logCtx log.Entry, client http.Client) *Provider {
	return &Provider{
		logCtx: logCtx,
		client: client,
	}
}

func NewHttpClient() *http.Client {
	//TODO:  Should timeout be configurable?
	c := &http.Client{
		Timeout: 15 * time.Second,
	}

	return c
}