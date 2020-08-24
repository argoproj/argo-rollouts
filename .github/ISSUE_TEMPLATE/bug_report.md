---
name: Bug report
about: Create a report to help us improve
labels: 'bug'
---
## Summary 

What happened/what you expected to happen?

## Diagnostics

What version of Argo Rollouts are you running?

```
# Paste the logs from the rollout controller

# Logs for the entire controller:
kubectl logs -n argo-rollouts deployment/argo-rollouts

# Logs for a specific rollout:
kubectl logs -n argo-rollouts deployment/argo-rollouts | grep rollout=<ROLLOUTNAME>
```

---
<!-- Issue Author: Don't delete this message to encourage other users to support your issue! -->
**Message from the maintainers**:

Impacted by this bug? Give it a 👍. We prioritize the issues with the most 👍.