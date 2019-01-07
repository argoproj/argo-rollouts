/*
Copyright 2017 The Kubernetes Authors.

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

package rollouts

const (
	Kind      string = "Rollout"
	Group     string = "argoproj.io"
	Singular  string = "rollout"
	Plural    string = "rollouts"
	ShortName string = "ro"
	FullName  string = Plural + "." + Group
)
