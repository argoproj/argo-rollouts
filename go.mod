module github.com/argoproj/argo-rollouts

go 1.20

require (
	github.com/antonmedv/expr v1.15.3
	github.com/argoproj/notifications-engine v0.4.1-0.20231011160156-2d2d1a75dbee
	github.com/argoproj/pkg v0.13.6
	github.com/aws/aws-sdk-go-v2 v1.21.2
	github.com/aws/aws-sdk-go-v2/config v1.19.1
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.27.2
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 v1.21.0
	github.com/blang/semver v3.5.1+incompatible
	github.com/bombsimon/logrusr/v4 v4.0.0
	github.com/evanphx/json-patch/v5 v5.6.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/mock v1.6.0
	github.com/golang/protobuf v1.5.3
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/hashicorp/go-plugin v1.4.10
	github.com/influxdata/influxdb-client-go/v2 v2.12.3
	github.com/juju/ansiterm v1.0.0
	github.com/machinebox/graphql v0.2.2
	github.com/mitchellh/mapstructure v1.5.0
	github.com/newrelic/newrelic-client-go v1.1.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.16.0
	github.com/prometheus/client_model v0.3.0
	github.com/prometheus/common v0.42.0
	github.com/prometheus/common/sigv4 v0.1.0
	github.com/servicemeshinterface/smi-sdk-go v0.5.0
	github.com/sirupsen/logrus v1.9.3
	github.com/soheilhy/cmux v0.1.5
	github.com/spaceapegames/go-wavefront v1.8.1
	github.com/spf13/cobra v1.7.0
	github.com/stretchr/testify v1.8.4
	github.com/tj/assert v0.0.3
	github.com/valyala/fasttemplate v1.2.2
	google.golang.org/genproto/googleapis/api v0.0.0-20230706204954-ccb25ca9f130
	google.golang.org/grpc v1.57.0
	google.golang.org/protobuf v1.31.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.25.8
	k8s.io/apiextensions-apiserver v0.25.8
	k8s.io/apimachinery v0.25.8
	k8s.io/apiserver v0.25.8
	k8s.io/cli-runtime v0.25.8
	k8s.io/client-go v0.25.8
	k8s.io/code-generator v0.25.8
	k8s.io/component-base v0.25.8
	k8s.io/klog/v2 v2.80.1
	k8s.io/kube-openapi v0.0.0-20221012153701-172d655c2280
	k8s.io/kubectl v0.25.8
	k8s.io/kubernetes v1.25.8
	k8s.io/utils v0.0.0-20230406110748-d93618cff8a2
	sigs.k8s.io/yaml v1.3.0
)

require (
	cloud.google.com/go/compute v1.20.1 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.27 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.20 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/logger v0.2.1 // indirect
	github.com/Azure/go-autorest/tracing v0.6.0 // indirect
	github.com/PagerDuty/go-pagerduty v1.7.0 // indirect
	github.com/bradleyfalzon/ghinstallation/v2 v2.5.0 // indirect
	github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1 // indirect
	github.com/google/go-github/v41 v41.0.0 // indirect
	github.com/matryer/is v1.4.0 // indirect
	github.com/russross/blackfriday v1.6.0 // indirect
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.2.0 // indirect
	github.com/Masterminds/sprig/v3 v3.2.3 // indirect
	github.com/ProtonMail/go-crypto v0.0.0-20230217124315-7d5c6f04bbb8 // indirect
	github.com/RocketChat/Rocket.Chat.Go.SDK v0.0.0-20220708192748-b73dcb041214 // indirect
	github.com/aws/aws-sdk-go v1.44.116 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.13.43 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.13.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.1.43 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.4.37 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.3.45 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.9.37 // indirect
	github.com/aws/aws-sdk-go-v2/service/sqs v1.20.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.15.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.17.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.23.2 // indirect
	github.com/aws/smithy-go v1.15.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/chai2010/gettext-go v1.0.2 // indirect
	github.com/cloudflare/circl v1.3.3 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/deepmap/oapi-codegen v1.11.0 // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/emicklei/go-restful/v3 v3.10.1 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/fatih/color v1.7.0 // indirect
	github.com/felixge/httpsnoop v1.0.3 // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32 // indirect
	github.com/go-errors/errors v1.4.2 // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/swag v0.21.1 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.0 // indirect
	github.com/golang/glog v1.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/go-github/v53 v53.0.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/s2a-go v0.1.4 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.2.5 // indirect
	github.com/googleapis/gax-go/v2 v2.12.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/gregdel/pushover v1.2.1 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v0.14.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.1 // indirect
	github.com/hashicorp/yamux v0.0.0-20180604194846-3520598351bb // indirect
	github.com/huandu/xstrings v1.3.3 // indirect
	github.com/imdario/mergo v0.3.13 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/influxdata/line-protocol v0.0.0-20210922203350-b1ad95c89adf // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/lunixbochs/vtclean v1.0.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-testing-interface v1.0.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/moby/term v0.0.0-20220808134915-39b0c02b01ae // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f // indirect
	github.com/oklog/run v1.0.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opsgenie/opsgenie-go-sdk-v2 v1.2.13 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/procfs v0.10.1 // indirect
	github.com/shopspring/decimal v1.2.0 // indirect
	github.com/slack-go/slack v0.12.2 // indirect
	github.com/spf13/cast v1.5.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/objx v0.5.0 // indirect
	github.com/tomnomnom/linkheader v0.0.0-20180905144013-02ca5825eb80 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fastjson v1.6.3 // indirect
	github.com/whilp/git-urls v0.0.0-20191001220047-6db9661140c0 // indirect
	github.com/xlab/treeprint v1.1.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.starlark.net v0.0.0-20200306205701-8dd3e2ee1dd5 // indirect
	golang.org/x/crypto v0.11.0 // indirect
	golang.org/x/mod v0.8.0 // indirect
	golang.org/x/net v0.12.0 // indirect
	golang.org/x/oauth2 v0.10.0 // indirect
	golang.org/x/sys v0.10.0 // indirect
	golang.org/x/term v0.10.0 // indirect
	golang.org/x/text v0.11.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	golang.org/x/tools v0.6.0 // indirect
	gomodules.xyz/envconfig v1.3.1-0.20190308184047-426f31af0d45 // indirect
	gomodules.xyz/notify v0.1.1 // indirect
	google.golang.org/api v0.132.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230706204954-ccb25ca9f130 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230711160842-782d3b101e98 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/cluster-bootstrap v0.25.8 // indirect
	k8s.io/component-helpers v0.25.8 // indirect
	k8s.io/gengo v0.0.0-20220902162205-c0856e24416d // indirect
	sigs.k8s.io/json v0.0.0-20220713155537-f223a00ba0e2 // indirect
	sigs.k8s.io/kustomize/api v0.12.1 // indirect
	sigs.k8s.io/kustomize/kyaml v0.13.9 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
)

replace (
	github.com/go-check/check => github.com/go-check/check v0.0.0-20180628173108-788fd7840127
	k8s.io/api v0.0.0 => k8s.io/api v0.25.8
	k8s.io/apiextensions-apiserver v0.0.0 => k8s.io/apiextensions-apiserver v0.25.8
	k8s.io/apimachinery v0.0.0 => k8s.io/apimachinery v0.25.8
	k8s.io/apiserver v0.0.0 => k8s.io/apiserver v0.25.8
	k8s.io/cli-runtime v0.0.0 => k8s.io/cli-runtime v0.25.8
	k8s.io/client-go v0.0.0 => k8s.io/client-go v0.25.8
	k8s.io/cloud-provider v0.0.0 => k8s.io/cloud-provider v0.25.8
	k8s.io/cluster-bootstrap v0.0.0 => k8s.io/cluster-bootstrap v0.25.8
	k8s.io/code-generator v0.0.0 => k8s.io/code-generator v0.25.8
	k8s.io/component-base v0.0.0 => k8s.io/component-base v0.25.8
	k8s.io/component-helpers v0.0.0 => k8s.io/component-helpers v0.25.8
	k8s.io/controller-manager v0.0.0 => k8s.io/controller-manager v0.25.8
	k8s.io/cri-api v0.0.0 => k8s.io/cri-api v0.25.8
	k8s.io/csi-translation-lib v0.0.0 => k8s.io/csi-translation-lib v0.25.8
	k8s.io/kube-aggregator v0.0.0 => k8s.io/kube-aggregator v0.25.8
	k8s.io/kube-controller-manager v0.0.0 => k8s.io/kube-controller-manager v0.25.8
	k8s.io/kube-proxy v0.0.0 => k8s.io/kube-proxy v0.25.8
	k8s.io/kube-scheduler v0.0.0 => k8s.io/kube-scheduler v0.25.8
	k8s.io/kubectl v0.0.0 => k8s.io/kubectl v0.25.8
	k8s.io/kubelet v0.0.0 => k8s.io/kubelet v0.25.8
	k8s.io/legacy-cloud-providers v0.0.0 => k8s.io/legacy-cloud-providers v0.25.8
	k8s.io/metrics v0.0.0 => k8s.io/metrics v0.25.8
	k8s.io/mount-utils v0.0.0 => k8s.io/mount-utils v0.25.8
	k8s.io/pod-security-admission v0.0.0 => k8s.io/pod-security-admission v0.25.8
	k8s.io/sample-apiserver v0.0.0 => k8s.io/sample-apiserver v0.25.8
)
