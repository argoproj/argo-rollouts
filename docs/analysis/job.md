# Job Metrics

A [Kubernetes Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/) can be used to run analysis. When a Job is used, the metric is considered
successful if the Job completes and had an exit code of zero, otherwise it is failed.

```yaml
metrics:
  - name: test
    provider:
      job:
        metadata:
          annotations:
            foo: bar # annotations defined here will be copied to the Job object
          labels:
            foo: bar # labels defined here will be copied to the Job object
        spec:
          backoffLimit: 1
          template:
            spec:
              containers:
                - name: test
                  image: my-image:latest
                  command:
                    [my-test-script, my-service.default.svc.cluster.local]
              restartPolicy: Never
```

The possible outcomes of a job metric are:

1. Jobs starts, runs and ends succesffully with exit 0. Analysis has passed
1. Jobs starts, runs and end with exit code non-zero. Analysis has failed
1. Job cannot start because of infrastructure error (e.g. image not found). Analysis is inconclusive
1. Job was still running but was terminated because it is not needed anymore.  Analysis is inconclusive

The last case can happen either when another metric has already failed or when the job is running as a background
analysis and the canary/blue-green process has finished.

## Control where the jobs run

Argo Rollouts allows you some control over where your metric job runs.

The following env vars can be set on the Rollouts controller:

`ARGO_ROLLOUTS_ANALYSIS_JOB_NAMESPACE` will allow you to run your metric jobs in a namespace other than the default (which can vary depending on if you are running Rollouts in cluster mode or not).

`ARGO_ROLLOUTS_ANALYSIS_JOB_KUBECONFIG` will allow running metric jobs in a different cluster entirely. This should be a path to the kubeconfig you want to use.
