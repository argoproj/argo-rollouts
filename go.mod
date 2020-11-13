module github.com/argoproj/argo-rollouts

go 1.13

require (
	4d63.com/gochecknoglobals v0.0.0-20201008074935-acfc0b28355a // indirect
	github.com/antonmedv/expr v1.8.9
	github.com/docker/docker v1.4.2-0.20190327010347-be7ac8be2ae0 // indirect
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/fatih/color v1.10.0 // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/go-openapi/spec v0.19.3
	github.com/goreleaser/goreleaser v0.146.0 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/jstemmer/go-junit-report v0.9.1
	github.com/juju/ansiterm v0.0.0-20180109212912-720a0952cc2a
	github.com/lunixbochs/vtclean v1.0.0 // indirect
	github.com/mbilski/exhaustivestruct v1.1.0 // indirect
	github.com/moricho/tparallel v0.2.1 // indirect
	github.com/newrelic/newrelic-client-go v0.47.1
	github.com/nishanths/exhaustive v0.1.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/polyfloyd/go-errorlint v0.0.0-20201006195004-351e25ade6e3 // indirect
	github.com/prometheus/client_golang v1.5.0
	github.com/prometheus/common v0.9.1
	github.com/securego/gosec/v2 v2.5.0 // indirect
	github.com/servicemeshinterface/smi-sdk-go v0.4.1
	github.com/sirupsen/logrus v1.7.0
	github.com/sourcegraph/go-diff v0.6.1 // indirect
	github.com/spaceapegames/go-wavefront v1.8.1
	github.com/spf13/cobra v1.1.1
	github.com/stretchr/testify v1.6.1
	github.com/tetafro/godot v0.4.9 // indirect
	github.com/tomarrell/wrapcheck v0.0.0-20200807122107-df9e8bcb914d // indirect
	github.com/undefinedlabs/go-mpatch v1.0.6
	github.com/valyala/fasttemplate v1.2.1
	github.com/valyala/quicktemplate v1.6.3 // indirect
	github.com/vektra/mockery v1.1.2
	golang.org/x/sys v0.0.0-20200826173525-f9321e4c35a6 // indirect
	golang.org/x/tools v0.0.0-20201013201025-64a9e34f3752 // indirect
	gopkg.in/yaml.v2 v2.3.0
	gopkg.in/yaml.v3 v3.0.0-20200615113413-eeeca48fe776 // indirect
	gotest.tools/v3 v3.0.3 // indirect
	honnef.co/go/tools v0.0.1-2020.1.6 // indirect
	k8s.io/api v0.18.2
	k8s.io/apiextensions-apiserver v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/apiserver v0.18.2
	k8s.io/cli-runtime v0.18.2
	k8s.io/client-go v0.18.2
	k8s.io/code-generator v0.18.2
	k8s.io/component-base v0.18.2
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20200121204235-bf4fb3bd569c
	k8s.io/kubectl v0.19.3
	k8s.io/kubernetes v1.18.2
	k8s.io/utils v0.0.0-20200324210504-a9aa75ae1b89
	mvdan.cc/gofumpt v0.0.0-20200802201014-ab5a8192947d // indirect
	mvdan.cc/unparam v0.0.0-20200501210554-b37ab49443f7 // indirect
	sigs.k8s.io/controller-tools v0.4.0
)

replace (
	k8s.io/api => k8s.io/api v0.18.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.18.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.3-beta.0
	k8s.io/apiserver => k8s.io/apiserver v0.18.2
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.18.2
	k8s.io/client-go => k8s.io/client-go v0.18.2
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.18.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.18.2
	k8s.io/code-generator => k8s.io/code-generator v0.18.3-beta.0
	k8s.io/component-base => k8s.io/component-base v0.18.2
	k8s.io/cri-api => k8s.io/cri-api v0.18.10-rc.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.18.2
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.18.2
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.18.2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.18.2
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.18.2
	k8s.io/kubectl => k8s.io/kubectl v0.18.2
	k8s.io/kubelet => k8s.io/kubelet v0.18.2
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.18.2
	k8s.io/metrics => k8s.io/metrics v0.18.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.18.2
)

replace k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.18.2

replace k8s.io/sample-controller => k8s.io/sample-controller v0.18.2
