# Kustomize Integration

Kustomize can be extended to understand CRD objects through the use of
[transformer configs](https://github.com/kubernetes-sigs/kustomize/tree/master/examples/transformerconfigs).
Using transformer configs, kustomize can be "taught" about the structure of a Rollout object and
leverage kustomize features such as ConfigMap/Secret generators, variable references, and common
labels & annotations. To use Rollouts with kustomize: 

1. Download [`rollout-transform.yaml`](kustomize/rollout-transform.yaml) into your kustomize directory.

2. Include `rollout-transform.yaml` in your kustomize `configurations` section:

```yaml
kind: Kustomization
apiVersion: kustomize.config.k8s.io/v1beta1

configurations:
- rollout-transform.yaml
```

An example kustomize app demonstrating the ability to use transformers with Rollouts can be seen
[here](https://github.com/argoproj/argo-rollouts/blob/master/docs/features/kustomize/example).

- With Kustomize 3.6.1 it is possible to reference the configuration directly from a remote resource:

```yaml
configurations:
  - https://argoproj.github.io/argo-rollouts/features/kustomize/rollout-transform.yaml
```

- With Kustomize 4.5.5 kustomize can use kubernetes OpenAPI data to get merge key and patch strategy information about [resource types](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/openapi). For example, given the following rollout:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-canary
spec:
  strategy:
    canary:
      steps:
      # detail of the canary steps is omitted
  template:
    metadata:
      labels:
        app: rollout-canary
    spec:
      containers:
      - name: rollouts-demo
        image: argoproj/rollouts-demo:blue
        imagePullPolicy: Always
        ports:
        - containerPort: 8080
```

user can update the Rollout via a patch in a kustomization file, to change the image to nginx

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
- rollout-canary.yaml

openapi:
  path: https://raw.githubusercontent.com/argoproj/argo-schema-generator/main/schema/argo_all_k8s_kustomize_schema.json

patchesStrategicMerge:
- |-
  apiVersion: argoproj.io/v1alpha1
  kind: Rollout
  metadata:
    name: rollout-canary
  spec:
    template:
      spec:
        containers:
        - name: rollouts-demo
          image: nginx
```

The OpenAPI data is auto-generated and defined in this [file](https://github.com/argoproj/argo-schema-generator/blob/main/schema/argo_all_k8s_kustomize_schema.json).

An example kustomize app demonstrating the ability to use OpenAPI data with Rollouts can be seen
[here](https://github.com/argoproj/argo-rollouts/blob/master/test/kustomize/rollout).
