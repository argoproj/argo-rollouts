module github.com/argoproj/argo-rollouts

go 1.16

require (
	github.com/antonmedv/expr v1.9.0
	github.com/argoproj/notifications-engine v0.3.0
	github.com/argoproj/pkg v0.9.0
	github.com/aws/aws-sdk-go-v2/config v1.13.0
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.5.0
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 v1.6.1
	github.com/blang/semver v3.5.1+incompatible
	github.com/evanphx/json-patch/v5 v5.6.0
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/gogo/protobuf v1.3.2
	github.com/golang/mock v1.4.4
	github.com/golang/protobuf v1.5.2
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/juju/ansiterm v0.0.0-20180109212912-720a0952cc2a
	github.com/lunixbochs/vtclean v1.0.0 // indirect
	github.com/mitchellh/mapstructure v1.3.3
	github.com/newrelic/newrelic-client-go v0.49.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.26.0
	github.com/servicemeshinterface/smi-sdk-go v0.4.1
	github.com/sirupsen/logrus v1.8.1
	github.com/soheilhy/cmux v0.1.5
	github.com/spaceapegames/go-wavefront v1.8.1
	github.com/spf13/cobra v1.1.3
	github.com/stretchr/testify v1.7.0
	github.com/tj/assert v0.0.3
	github.com/valyala/fasttemplate v1.2.1
	golang.org/x/sys v0.0.0-20211205182925-97ca703d548d // indirect
	google.golang.org/genproto v0.0.0-20210602131652-f16073e35f0c
	google.golang.org/grpc v1.38.0
	google.golang.org/protobuf v1.26.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.5
	k8s.io/apiextensions-apiserver v0.21.0
	k8s.io/apimachinery v0.22.5
	k8s.io/apiserver v0.22.5
	k8s.io/cli-runtime v0.22.5
	k8s.io/client-go v0.22.5
	k8s.io/code-generator v0.22.5
	k8s.io/component-base v0.22.5
	k8s.io/klog/v2 v2.9.0
	k8s.io/kube-openapi v0.0.0-20211109043538-20434351676c
	k8s.io/kubectl v0.21.0
	k8s.io/kubernetes v1.22.5
	k8s.io/utils v0.0.0-20210819203725-bdf08cb9a70a
)

replace (
	github.com/go-check/check => github.com/go-check/check v0.0.0-20180628173108-788fd7840127
	github.com/grpc-ecosystem/grpc-gateway => github.com/grpc-ecosystem/grpc-gateway v1.16.0
	k8s.io/api => k8s.io/api v0.22.5
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.22.5
	k8s.io/apimachinery => k8s.io/apimachinery v0.22.6-rc.0
	k8s.io/apiserver => k8s.io/apiserver v0.22.5
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.22.5
	k8s.io/client-go => k8s.io/client-go v0.22.5
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.22.5
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.22.5
	k8s.io/code-generator => k8s.io/code-generator v0.22.6-rc.0
	k8s.io/component-base => k8s.io/component-base v0.22.5
	k8s.io/component-helpers => k8s.io/component-helpers v0.22.5
	k8s.io/controller-manager => k8s.io/controller-manager v0.22.5
	k8s.io/cri-api => k8s.io/cri-api v0.22.6-rc.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.22.5
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.22.5
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.22.5
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.22.5
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.22.5
	k8s.io/kubectl => k8s.io/kubectl v0.22.5
	k8s.io/kubelet => k8s.io/kubelet v0.22.5
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.22.5
	k8s.io/metrics => k8s.io/metrics v0.22.5
	k8s.io/mount-utils => k8s.io/mount-utils v0.22.6-rc.0
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.22.5
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.22.5
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.22.5
	k8s.io/sample-controller => k8s.io/sample-controller v0.22.5
)
