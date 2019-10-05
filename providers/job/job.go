package job

import (
	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/client-go/listers/batch/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	ProviderType = "job"
)

type Provider struct {
	jobLister batchv1.JobLister
	logCtx    log.Entry
}

func NewProvider(logCtx log.Entry, jobLister batchv1.JobLister) *Provider {
	return &Provider{
		logCtx:    logCtx,
		jobLister: jobLister,
	}
}

func (p *Provider) Type() string {
	return ProviderType
}

func (p *Provider) Run(metric v1alpha1.Metric, args []v1alpha1.Argument) (v1alpha1.Measurement, error) {
	var measurement = v1alpha1.Measurement{
		Metadata: map[string]string{},
	}
	return measurement, nil
}

func (p *Provider) Resume(metric v1alpha1.Metric, args []v1alpha1.Argument, measurement v1alpha1.Measurement) (v1alpha1.Measurement, error) {
	return measurement, nil
}

func (p *Provider) Terminate(metric v1alpha1.Metric, args []v1alpha1.Argument, measurement v1alpha1.Measurement) (v1alpha1.Measurement, error) {
	return measurement, nil
}
