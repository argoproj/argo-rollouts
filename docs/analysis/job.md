# Job Metrics

A Kubernetes Job can be used to run analysis. When a Job is used, the metric is considered
successful if the Job completes and had an exit code of zero, otherwise it is failed.

```yaml
  metrics:
  - name: test
    provider:
      job:
        spec:
          backoffLimit: 1
          template:
            spec:
              containers:
              - name: test
                image: my-image:latest
                command: [my-test-script, my-service.default.svc.cluster.local]
              restartPolicy: Never
```
