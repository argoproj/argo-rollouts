package unstructured

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestStrToUnstructuredSuccessfully(t *testing.T) {
	rsStr := `apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: test
spec:
  replicas: 1
`
	t.Run("Safe method", func(t *testing.T) {
		obj, err := StrToUnstructured(rsStr)
		assert.Nil(t, err)
		assert.NotNil(t, obj)
		assert.Equal(t, "test", obj.GetName())
		assert.Equal(t, "ReplicaSet", obj.GetKind())
		assert.Equal(t, "apps/v1", obj.GetAPIVersion())
		replicas, exists, err := unstructured.NestedFloat64(obj.Object, "spec", "replicas")
		assert.True(t, exists)
		assert.Nil(t, err)
		assert.Equal(t, float64(1), replicas)
	})

	t.Run("Unsafe method", func(t *testing.T) {
		obj := StrToUnstructuredUnsafe(rsStr)
		assert.NotNil(t, obj)
		assert.Equal(t, "test", obj.GetName())
		assert.Equal(t, "ReplicaSet", obj.GetKind())
		assert.Equal(t, "apps/v1", obj.GetAPIVersion())
		replicas, exists, err := unstructured.NestedFloat64(obj.Object, "spec", "replicas")
		assert.True(t, exists)
		assert.Nil(t, err)
		assert.Equal(t, float64(1), replicas)
	})
}

func TestStrToUnstructuredFails(t *testing.T) {
	t.Run("Safe method", func(t *testing.T) {
		obj, err := StrToUnstructured("{")
		assert.Nil(t, obj)
		assert.NotNil(t, err)
	})

	t.Run("Unsafe method", func(t *testing.T) {
		var obj *unstructured.Unstructured
		assert.Panics(t, func() {
			obj = StrToUnstructuredUnsafe("{")
		})
		assert.Nil(t, obj)
	})
}

func TestSplitYAML(t *testing.T) {
	rsStr := `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: test1
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: test2
spec:
  replicas: 2
`
	uns, err := SplitYAML(rsStr)
	assert.NoError(t, err)
	assert.Len(t, uns, 2)

	{
		obj := uns[0]
		assert.Equal(t, "test1", obj.GetName())
		assert.Equal(t, "ReplicaSet", obj.GetKind())
		assert.Equal(t, "apps/v1", obj.GetAPIVersion())
		replicas, exists, err := unstructured.NestedFloat64(obj.Object, "spec", "replicas")
		assert.True(t, exists)
		assert.Nil(t, err)
		assert.Equal(t, float64(1), replicas)
	}
	{
		obj := uns[1]
		assert.Equal(t, "test2", obj.GetName())
		assert.Equal(t, "ReplicaSet", obj.GetKind())
		assert.Equal(t, "apps/v1", obj.GetAPIVersion())
		replicas, exists, err := unstructured.NestedFloat64(obj.Object, "spec", "replicas")
		assert.True(t, exists)
		assert.Nil(t, err)
		assert.Equal(t, float64(2), replicas)
	}
}

func TestObjectToRollout(t *testing.T) {
	roYAML := `
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: basic
spec:
  strategy:
    canary: {}
    matchLabels:
      app: basic
  template:
    metadata:
      labels:
        app: basic
    spec:
      containers:
      - name: rollouts-demo
        image: argoproj/rollouts-demo:blue
`
	obj, err := StrToUnstructured(roYAML)
	assert.NotNil(t, obj)
	assert.NoError(t, err)
	ro := ObjectToRollout(obj)
	assert.NotNil(t, ro)
	ro2 := ObjectToRollout(ro)
	assert.Equal(t, ro, ro2)
	var invalid struct{}
	ro3 := ObjectToRollout(&invalid)
	assert.Nil(t, ro3)
}

func TestObjectToAnalysisRun(t *testing.T) {
	arYAML := `
kind: AnalysisRun
apiVersion: argoproj.io/v1alpha1
metadata:
  generateName: analysis-run-job-
spec:
  metrics:
  - name: test
    provider:
      job:
        spec:
          template:
            spec:
              containers:
              - name: sleep
                image: alpine:3.8
                command: [sleep, "30"]
              restartPolicy: Never
          backoffLimit: 0
`
	obj, err := StrToUnstructured(arYAML)
	assert.NotNil(t, obj)
	assert.NoError(t, err)
	ar := ObjectToAnalysisRun(obj)
	assert.NotNil(t, ar)
	ar2 := ObjectToAnalysisRun(ar)
	assert.Equal(t, ar, ar2)
	var invalid struct{}
	ar3 := ObjectToAnalysisRun(&invalid)
	assert.Nil(t, ar3)
}

func TestObjectToExpirment(t *testing.T) {
	exYAML := `
apiVersion: argoproj.io/v1alpha1
kind: Experiment
metadata:
  name: experiment-with-analysis
spec:
  templates:
  - name: purple
    selector:
      matchLabels:
        app: rollouts-demo
    template:
      metadata:
        labels:
          app: rollouts-demo
      spec:
        containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:purple
          imagePullPolicy: Always
  - name: orange
    selector:
      matchLabels:
        app: rollouts-demo
    template:
      metadata:
        labels:
          app: rollouts-demo
      spec:
        containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:orange
          imagePullPolicy: Always
  analyses:
  - name: random-fail
    templateName: random-fail
  - name: pass
    templateName: pass
`
	obj, err := StrToUnstructured(exYAML)
	assert.NotNil(t, obj)
	assert.NoError(t, err)
	ex := ObjectToExperiment(obj)
	assert.NotNil(t, ex)
	ex2 := ObjectToExperiment(ex)
	assert.Equal(t, ex, ex2)
	var invalid struct{}
	ex3 := ObjectToExperiment(&invalid)
	assert.Nil(t, ex3)
}
