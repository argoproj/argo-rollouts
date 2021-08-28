# CloudWatch Metrics

!!! important
    Available since v1.1.0

A [CloudWatch](https://aws.amazon.com/cloudwatch/) using [GetMetricData](https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_GetMetricData.html) can be used to obtain measurements for analysis.

## Setup

You can use CloudWatch Metrics if you have used to EKS or not. This analysis is required IAM permission for `cloudwatch:GetMetricData` and you need to define `AWS_REGION` in Deployment for `argo-rollouts`.

### EKS

If you create new cluster on EKS, you can attach [cluster IAM role](https://docs.aws.amazon.com/eks/latest/userguide/service_IAM_role.html) or attach [IAM roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).  
If you have already cluster on EKS, you can attach [IAM roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

### not EKS

You need to define access key and secret key.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cloudwatch-secret
type: Opaque
stringData:
  AWS_ACCESS_KEY_ID: <aws-access-key-id>
  AWS_SECRET_ACCESS_KEY: <aws-secret-access-key>
  AWS_REGION: <aws-region>
```

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argo-rollouts
spec:
  template:
    spec:
      containers:
      - name: argo-rollouts
        env:
        - name: AWS_ACCESS_KEY_ID
          valueFrom:
            secretKeyRef:
              name: cloudwatch-secret
              key: AWS_ACCESS_KEY_ID
        - name: AWS_SECRET_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: cloudwatch-secret
              key: AWS_SECRET_ACCESS_KEY
        - name: AWS_REGION
          valueFrom:
            secretKeyRef:
              name: cloudwatch-secret
              key: AWS_REGION
```

## Configuration

- `metricDataQueries` - GetMetricData query: [MetricDataQuery](https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_MetricDataQuery.html)
- `interval` - optional interval, e.g. 15m, default: 5m

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
spec:
  metrics:
  - name: success-rate
    interval: 1m
    successCondition: "all(result[0].Values, {# <= 0.01})"
    failureLimit: 3
    provider:
      cloudWatch:
        interval: 15m
        metricDataQueries:
        - {
            "Id": "rate",
            "Expression": "errors / requests"
          }
        - {
            "Id": "errors",
            "MetricStat": {
              "Metric": {
                "Namespace": "app",
                "MetricName": "errors"
              },
              "Period": 300,
              "Stat": "Sum",
              "Unit": "Count"
            },
            "ReturnData": false
          }
        - {
            "Id": "requests",
            "MetricStat": {
              "Metric": {
                "Namespace": "app",
                "MetricName": "requests"
              },
              "Period": 300,
              "Stat": "Sum",
              "Unit": "Count"
            },
            "ReturnData": false
          }
```

## debug

You can confirm the results value in `AnalysisRun`.

```bash
kubectl get analysisrun/rollouts-name-xxxxxxxxxx-xx -o json | jq ".status.metricResults"
```
