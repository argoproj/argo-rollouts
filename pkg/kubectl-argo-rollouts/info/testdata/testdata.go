package testdata

import (
	"io/ioutil"
	"path"
	"runtime"
	"time"

	"github.com/ghodss/yaml"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

var testDir string

func init() {
	_, filename, _, _ := runtime.Caller(0)
	// The ".." may change depending on you folder structure
	testDir = path.Join(path.Dir(filename))
}

type RolloutObjects struct {
	Rollouts     []*v1alpha1.Rollout
	ReplicaSets  []*appsv1.ReplicaSet
	Pods         []*corev1.Pod
	Experiments  []*v1alpha1.Experiment
	AnalysisRuns []*v1alpha1.AnalysisRun
}

func (r *RolloutObjects) AllObjects() []k8sruntime.Object {
	var objs []k8sruntime.Object
	for _, o := range r.Rollouts {
		objs = append(objs, o)
	}
	for _, o := range r.ReplicaSets {
		objs = append(objs, o)
	}
	for _, o := range r.Pods {
		objs = append(objs, o)
	}
	for _, o := range r.Experiments {
		objs = append(objs, o)
	}
	for _, o := range r.AnalysisRuns {
		objs = append(objs, o)
	}
	return objs
}

/* NewCanaryRollout() returns the following canary rollout and related objects

Name:            canary-demo
Namespace:       jesse-test
Status:          ✖ Degraded
Strategy:        Canary
  Step:          0/8
  SetWeight:     20
  ActualWeight:  0
Images:          argoproj/rollouts-demo:green
Replicas:
  Desired:       5
  Current:       6
  Updated:       1
  Ready:         5
  Available:     5

NAME                                    KIND        STATUS              INFO       AGE
⟳ canary-demo                           Rollout     ✖ Degraded                     7d
├───⧉ canary-demo-65fb5ffc84 (rev:31)   ReplicaSet  ◷ Progressing       canary     7d
│   └───□ canary-demo-65fb5ffc84-9wf5r  Pod         ✖ ImagePullBackOff  ready:0/1  7d
├───⧉ canary-demo-877894d5b (rev:30)    ReplicaSet  ✔ Healthy           stable     7d
│   ├───□ canary-demo-877894d5b-6jfpt   Pod         ✔ Running           ready:1/1  7d
│   ├───□ canary-demo-877894d5b-kh7x4   Pod         ✔ Running           ready:1/1  7d
│   ├───□ canary-demo-877894d5b-7jmqw   Pod         ✔ Running           ready:1/1  7d
│   ├───□ canary-demo-877894d5b-j8g2b   Pod         ✔ Running           ready:1/1  7d
│   └───□ canary-demo-877894d5b-jw5qm   Pod         ✔ Running           ready:1/1  7d
├───⧉ canary-demo-859c99b45c (rev:29)   ReplicaSet  • ScaledDown                   7d
*/
func NewCanaryRollout() *RolloutObjects {
	return discoverObjects(testDir + "/canary")
}

/* NewBlueGreenRollout returns the following rollout and related objects

Name:            bluegreen-demo
Namespace:       jesse-test
Status:          ‖ Paused
Strategy:        BlueGreen
Images:          argoproj/rollouts-demo:blue
                 argoproj/rollouts-demo:green
Replicas:
  Desired:       3
  Current:       6
  Updated:       3
  Ready:         6
  Available:     3

NAME                                       KIND        STATUS        INFO       AGE
⟳ bluegreen-demo                           Rollout     ‖ Paused                 7d
├───⧉ bluegreen-demo-74b948fccb (rev:11)   ReplicaSet  ✔ Healthy     preview    7d
│   ├───□ bluegreen-demo-74b948fccb-5jz59  Pod         ✔ Running     ready:1/1  7d
│   ├───□ bluegreen-demo-74b948fccb-mkhrl  Pod         ✔ Running     ready:1/1  7d
│   └───□ bluegreen-demo-74b948fccb-vvj2t  Pod         ✔ Running     ready:1/1  7d
├───⧉ bluegreen-demo-6cbccd9f99 (rev:10)   ReplicaSet  ✔ Healthy     active     7d
│   ├───□ bluegreen-demo-6cbccd9f99-gk78v  Pod         ✔ Running     ready:1/1  7d
│   ├───□ bluegreen-demo-6cbccd9f99-kxj8g  Pod         ✔ Running     ready:1/1  7d
│   └───□ bluegreen-demo-6cbccd9f99-t2d4f  Pod         ✔ Running     ready:1/1  7d
├───⧉ bluegreen-demo-746d5fddf6 (rev:8)    ReplicaSet  • ScaledDown             7d
*/
func NewBlueGreenRollout() *RolloutObjects {
	return discoverObjects(testDir + "/blue-green")
}

func discoverObjects(path string) *RolloutObjects {
	files, err := ioutil.ReadDir(path)
	if err != nil {
		panic(err)
	}
	// we set creation timestamp so that AGE output in CLI can be compared
	aWeekAgo := metav1.Time{
		Time: time.Now().Add(-7 * 24 * time.Hour).Truncate(time.Second),
	}

	var objs RolloutObjects
	for _, file := range files {
		yamlBytes, err := ioutil.ReadFile(path + "/" + file.Name())
		if err != nil {
			panic(err)
		}
		typeMeta := GetTypeMeta(yamlBytes)
		switch typeMeta.Kind {
		case "Rollout":
			var ro v1alpha1.Rollout
			err = yaml.Unmarshal(yamlBytes, &ro)
			if err != nil {
				panic(err)
			}
			ro.CreationTimestamp = aWeekAgo
			objs.Rollouts = append(objs.Rollouts, &ro)
		case "ReplicaSet":
			var rs appsv1.ReplicaSet
			err = yaml.Unmarshal(yamlBytes, &rs)
			if err != nil {
				panic(err)
			}
			rs.CreationTimestamp = aWeekAgo
			objs.ReplicaSets = append(objs.ReplicaSets, &rs)
		case "Pod":
			var pod corev1.Pod
			err = yaml.Unmarshal(yamlBytes, &pod)
			if err != nil {
				panic(err)
			}
			pod.CreationTimestamp = aWeekAgo
			objs.Pods = append(objs.Pods, &pod)
		case "Experiment":
			var exp v1alpha1.Experiment
			err = yaml.Unmarshal(yamlBytes, &exp)
			if err != nil {
				panic(err)
			}
			exp.CreationTimestamp = aWeekAgo
			objs.Experiments = append(objs.Experiments, &exp)
		case "AnalysisRun":
			var run v1alpha1.AnalysisRun
			err = yaml.Unmarshal(yamlBytes, &run)
			if err != nil {
				panic(err)
			}
			run.CreationTimestamp = aWeekAgo
			objs.AnalysisRuns = append(objs.AnalysisRuns, &run)
		}
	}
	return &objs
}

func GetTypeMeta(yamlBytes []byte) metav1.TypeMeta {
	type k8sObj struct {
		metav1.TypeMeta `json:",inline"`
	}
	var o k8sObj
	err := yaml.Unmarshal(yamlBytes, &o)
	if err != nil {
		panic(err)
	}
	return o.TypeMeta
}
