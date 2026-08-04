---
name: Bug report
about: Create a report to help us improve
title: ''
labels: 'bug'
assignees: ''
---

<!-- If you are trying to resolve an environment-specific issue or have a one-off question about the edge case that does not require a feature then please consider asking a question in argo rollouts slack [channel](https://argoproj.github.io/community/join-slack). -->

Checklist:

* [ ] I've included steps to reproduce the bug.
* [ ] I've included the version of argo rollouts.

**Describe the bug**

<!-- A clear and concise description of what the bug is. -->

**To Reproduce**

<!-- A list of the steps required to reproduce the issue. Best of all, give us the URL to a repository that exhibits this issue. -->

**Expected behavior**

<!-- A clear and concise description of what you expected to happen. -->

**Screenshots**

<!-- If applicable, add screenshots to help explain your problem. -->

**Version**

<!-- What version of argo rollouts controller are you running? -->

**Logs**

```
# Paste the logs from the rollout controller

# Logs for the entire controller:
kubectl logs -n argo-rollouts deployment/argo-rollouts

# Logs for a specific rollout:
kubectl logs -n argo-rollouts deployment/argo-rollouts | grep rollout=<ROLLOUTNAME
```

---
<!-- Issue Author: Don't delete this message to encourage other users to support your issue! -->
**Message from the maintainers**:

Impacted by this bug? Give it a 👍. We prioritize the issues with the most 👍.
