package validation

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"reflect"
	"testing"
)


func TestValidateAnalysisTemplateWithType(t *testing.T) {
	//AnalysisTemplateWithType{
	//	AnalysisTemplate: nil,
	//	TemplateType:     "",
	//	AnalysisIndex:    0,
	//	CanaryStepIndex:  0,
	//}

	//type args struct {
	//	template AnalysisTemplateWithType
	//}
	//tests := []struct {
	//	name string
	//	args args
	//	want field.ErrorList
	//}{
	//	// TODO: Add test cases.
	//}
	//for _, tt := range tests {
	//	t.Run(tt.name, func(t *testing.T) {
	//		if got := ValidateAnalysisTemplateWithType(tt.args.template); !reflect.DeepEqual(got, tt.want) {
	//			t.Errorf("ValidateAnalysisTemplateWithType() = %v, want %v", got, tt.want)
	//		}
	//	})
	//}
}

func TestValidateIngress(t *testing.T) {
	type args struct {
		rollout *v1alpha1.Rollout
		ingress v1beta1.Ingress
	}
	tests := []struct {
		name string
		args args
		want field.ErrorList
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateIngress(tt.args.rollout, tt.args.ingress); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ValidateIngress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateRolloutReferencedResources(t *testing.T) {
	type args struct {
		rollout             *v1alpha1.Rollout
		referencedResources ReferencedResources
	}
	tests := []struct {
		name string
		args args
		want field.ErrorList
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateRolloutReferencedResources(tt.args.rollout, tt.args.referencedResources); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ValidateRolloutReferencedResources() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateService(t *testing.T) {
	type args struct {
		svc     ServiceWithType
		rollout *v1alpha1.Rollout
	}
	tests := []struct {
		name string
		args args
		want field.ErrorList
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateService(tt.args.svc, tt.args.rollout); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ValidateService() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateVirtualService(t *testing.T) {

}

func TestGetAnalysisTemplateWithTypeFieldPath(t *testing.T) {
	fldPath := GetAnalysisTemplateWithTypeFieldPath(PrePromotionAnalysis, 0, 0)
	assert.Equal(t, "spec.strategy.blueGreen.prePromotionAnalysis.templates[0].templateName", fldPath.String())

	fldPath = GetAnalysisTemplateWithTypeFieldPath(PostPromotionAnalysis, 0, 0)
	assert.Equal(t, "spec.strategy.blueGreen.postPromotionAnalysis.templates[0].templateName", fldPath.String())

	fldPath = GetAnalysisTemplateWithTypeFieldPath(CanaryStep, 0, 0)
	assert.Equal(t, "spec.strategy.canary.steps[0].analysis.templates[0].templateName", fldPath.String())

	fldPath = GetAnalysisTemplateWithTypeFieldPath("DoesNotExist", 0, 0)
	assert.Nil(t, fldPath)
}

func TestGetServiceWithTypeFieldPath(t *testing.T) {
	fldPath := GetServiceWithTypeFieldPath(ActiveService)
	assert.Equal(t, "spec.strategy.blueGreen.activeService", fldPath.String())

	fldPath = GetServiceWithTypeFieldPath(PreviewService)
	assert.Equal(t, "spec.strategy.blueGreen.previewService", fldPath.String())

	fldPath = GetServiceWithTypeFieldPath(CanaryService)
	assert.Equal(t, "spec.strategy.canary.canaryService", fldPath.String())

	fldPath = GetServiceWithTypeFieldPath(StableService)
	assert.Equal(t, "spec.strategy.canary.stableService", fldPath.String())

	fldPath = GetServiceWithTypeFieldPath("DoesNotExist")
	assert.Nil(t, fldPath)
}