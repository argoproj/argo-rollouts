/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	internalinterfaces "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// AnalysisRuns returns a AnalysisRunInformer.
	AnalysisRuns() AnalysisRunInformer
	// AnalysisTemplates returns a AnalysisTemplateInformer.
	AnalysisTemplates() AnalysisTemplateInformer
	// ClusterAnalysisTemplates returns a ClusterAnalysisTemplateInformer.
	ClusterAnalysisTemplates() ClusterAnalysisTemplateInformer
	// Experiments returns a ExperimentInformer.
	Experiments() ExperimentInformer
	// IngressRoutes returns a IngressRouteInformer.
	IngressRoutes() IngressRouteInformer
	// Rollouts returns a RolloutInformer.
	Rollouts() RolloutInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// AnalysisRuns returns a AnalysisRunInformer.
func (v *version) AnalysisRuns() AnalysisRunInformer {
	return &analysisRunInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// AnalysisTemplates returns a AnalysisTemplateInformer.
func (v *version) AnalysisTemplates() AnalysisTemplateInformer {
	return &analysisTemplateInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// ClusterAnalysisTemplates returns a ClusterAnalysisTemplateInformer.
func (v *version) ClusterAnalysisTemplates() ClusterAnalysisTemplateInformer {
	return &clusterAnalysisTemplateInformer{factory: v.factory, tweakListOptions: v.tweakListOptions}
}

// Experiments returns a ExperimentInformer.
func (v *version) Experiments() ExperimentInformer {
	return &experimentInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// IngressRoutes returns a IngressRouteInformer.
func (v *version) IngressRoutes() IngressRouteInformer {
	return &ingressRouteInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// Rollouts returns a RolloutInformer.
func (v *version) Rollouts() RolloutInformer {
	return &rolloutInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}
