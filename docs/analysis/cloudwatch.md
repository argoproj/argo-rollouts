# CloudWatch Metrics

!!! important
    Available since v1.1.0

A [CloudWatch](https://aws.amazon.com/cloudwatch/) using [GetMetricData](https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_GetMetricData.html) can be used to obtain measurements for analysis.

## Setup

You can use CloudWatch Metrics if you have used to EKS or not.

### EKS

If you create new cluster on EKS, you need to attach [cluster IAM role](https://docs.aws.amazon.com/eks/latest/userguide/service_IAM_role.html) or attach [IAM roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).  
If you have already cluster on EKS, you need to attach [IAM roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

### not EKS

You need to define access key and secret key.

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
        metricDataQueries: |
          [
            {
              "Id": "rate",
              "Expression": "errors / requests"
            },
            {
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
            },
            {
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
          ]
```
