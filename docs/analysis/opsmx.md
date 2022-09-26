## OpsMx


Analysis using OpsMx Metric Provider can be a part of an [Experiment](../features/experiment.md) and a [Canary](../features/canary.md).

This example is focused on a canary based rollout performing analysis using OpsMx Metric Provider after each progressive step.

The example demonstrates:
- The ability to run an analysis during canary rollout strategy.
- The ability of OpsMx metric provider to consume pod hash values to filter monitoring data from data sources.
- OpsMx Metrics and Log Based Analysis.

=== "Rollout"
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: oes-argo-rollout
spec:
  replicas: 4
  revisionHistoryLimit: 2
  selector:
    matchLabels:
      app: testapp-rollout
  template:
    metadata:
      annotations:
        prometheus.io/scrape: 'true'
        prometheus_io_path: '/mgmt/prometheus'
        prometheus_io_port: '8088'    
      labels:
        app: testapp-rollout
    spec:
      containers:
        - name: rollouts-baseline
          image: docker.io/opsmxdev/issuegen:v3.0.0
          imagePullPolicy: Always
          ports:
            - containerPort: 8088
  strategy:
    canary:
      steps:
        - setWeight: 25
        - pause: { duration: 30s }
        - analysis:
            templates:
              - templateName: oes-analysis-for-canary
            args:
              - name: canary-hash
                valueFrom:
                  podTemplateHashValue: Latest
              - name: baseline-hash
                valueFrom:
                  podTemplateHashValue: Stable
        - setWeight: 50
        - pause: { duration: 30s }
        - analysis:
            templates:
              - templateName: oes-analysis-for-canary
            args:
              - name: canary-hash
                valueFrom:
                  podTemplateHashValue: Latest
              - name: baseline-hash
                valueFrom:
                  podTemplateHashValue: Stable
        - setWeight: 100
```

=== "Analysis Template"

```yaml
kind: AnalysisTemplate
apiVersion: argoproj.io/v1alpha1
metadata:
  name: oes-analysis-for-canary
spec:
  args:
    - name: canary-hash
    - name: baseline-hash
  metrics:
    - name: oes-analysis-for-canary
      count: 1
      initialDelay: 10s
      provider:
        opsmx:
          gateUrl: https://gate-endpoint.opsmx.net/
          application: testappforcanary
          user: testuser
          lifetimeHours: "0.05"
          threshold:
            pass: 80
            marginal: 60
          services:
          - serviceName: demoserviceforisd
            metricTemplateName: PrometheusMetricTemplate
            metricScopeVariables: "${namespace_key},${pod_key},${app_name}"
            baselineMetricScope: "argocd,.*{{args.baseline-hash}}.*,testapp-rollout"
            canaryMetricScope: "argocd,.*{{args.canary-hash}}.*,testapp-rollout"
            logTemplateName: ElasticLogTemplate
            logScopeVariables: "kubernetes.pod_name"
            baselineLogScope: ".*{{args.baseline-hash}}.*"
            canaryLogScope: ".*{{args.canary-hash}}.*"
```

The use cases starts with the baseline ReplicaSet running with size of 4 replicas. As soon as the rollout begins on updation of the application image, the rollout strategy steps are executed:

1. Rollout begins with shifting 25% load to canary replica set. To achieve this 1 pod of canary replica set is scaled up and the baseline replica set is scaled down to 3 from 4 replicas.

2. A pause of 30s to stabilize the environment.

3. Analysis using OpsMx provider. The provider is supplied with pod hash values of Latest and Stable replica sets to filter the data from monitoring systems based on baseline and canary pod hash values. This example uses Prometheus as data source for metrics and ElasticSearch for logs.

4. Upon successful execution of analysis, next progressive step to release 50% of user traffic to canary replica set is executed. To achieve this, the baseline replica set is scaled down to 2 replicas from 3. And the canary replica set is scaled up to 2 replicas from 1.

5. Another pause of 30s to stabilize the environment.

6. Another analysis with new size of replica sets in a similar way as step 3 is executed.

7. Complete rollout upon successful analysis.

In case, step 3 or step 6 fails, immediate rollback is initiated.
