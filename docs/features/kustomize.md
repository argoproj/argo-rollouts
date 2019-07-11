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

A example kustomize app demonstrating the ability to use transformers with Rollouts can be seen [here](./kustomize).
