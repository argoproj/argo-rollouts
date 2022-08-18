
# Vertical Pod Autoscaling  

Vertical Pod Autoscaling (VPA) reduces the maintenance cost and improve utilization of cluster resources by automating configuration of resource requirements.  
  
## VPA modes 
 
There are four modes in which VPAs operate  
  
1. "Auto": VPA assigns resource requests on pod creation as well as updates them on existing pods using the preferred update mechanism. Currently this is equivalent to "Recreate" (see below). Once restart free ("in-place") update of pod requests is available, it may be used as the preferred update mechanism by the "Auto" mode. 
NOTE: This feature of VPA is experimental and may cause downtime for your applications.
  
1. "Recreate": VPA assigns resource requests on pod creation as well as updates them on existing pods by evicting them when the requested resources differ significantly from the new recommendation (respecting the Pod Disruption Budget, if defined). This mode should be used rarely, only if you need to ensure that the pods are restarted whenever the resource request changes. Otherwise prefer the "Auto" mode which may take advantage of restart free updates once they are available. 
NOTE: This feature of VPA is experimental and may cause downtime for your applications.

1. "Initial": VPA only assigns resource requests on pod creation and never changes them later.  

1. "Off": VPA does not automatically change resource requirements of the pods. The recommendations are calculated and can be inspected in the VPA object. 
  
  
## Example  
  
Below is an example of a Vertical Pod Autoscaler with Argo-Rollouts.
  
Rollout sample app:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: vpa-demo-rollout
  namespace: test-vpa
spec:
  replicas: 5
  strategy:
    canary:
      steps:
      - setWeight: 20
      - pause: {duration: 10}
      - setWeight: 40
      - pause: {duration: 10}
      - setWeight: 60
      - pause: {duration: 10}
      - setWeight: 80
      - pause: {duration: 10}
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app: vpa-demo-rollout
  template:
    metadata:
      labels:
        app: vpa-demo-rollout
    spec:
      containers:
      - name: vpa-demo-rollout
        image: ravihari/nginx:v1
        ports:
        - containerPort: 80
        resources:
          requests:
            cpu: "5m"       
            memory: "5Mi" 
```

VPA configuration for Rollout sample app:

```yaml  
apiVersion: "autoscaling.k8s.io/v1beta2"
kind: VerticalPodAutoscaler  
metadata:  
  name: vpa-rollout-example  
  namespace: test-vpa  
spec:  
  targetRef:  
    apiVersion: "argoproj.io/v1alpha1"  
    kind: Rollout  
    name: vpa-demo-rollout  
  updatePolicy:  
    updateMode: "Auto"  
  resourcePolicy:  
    containerPolicies:  
    - containerName: '*'  
    minAllowed:  
      cpu: 5m  
      memory: 5Mi  
    maxAllowed:  
      cpu: 1  
      memory: 500Mi  
    controlledResources: ["cpu", "memory"]  
```

Describe VPA when initially deployed we donot see recommendations as it will take few mins.

```yaml
Name:         kubengix-vpa
Namespace:    test-vpa
Labels:       <none>
Annotations:  <none>
API Version:  autoscaling.k8s.io/v1
Kind:         VerticalPodAutoscaler
Metadata:
  Creation Timestamp:  2022-03-14T12:54:06Z
  Generation:          1
  Managed Fields:
    API Version:  autoscaling.k8s.io/v1beta2
    Fields Type:  FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          .:
          f:kubectl.kubernetes.io/last-applied-configuration:
      f:spec:
        .:
        f:resourcePolicy:
          .:
          f:containerPolicies:
        f:targetRef:
          .:
          f:apiVersion:
          f:kind:
          f:name:
        f:updatePolicy:
          .:
          f:updateMode:
    Manager:         kubectl-client-side-apply
    Operation:       Update
    Time:            2022-03-14T12:54:06Z
  Resource Version:  3886
  UID:               4ac64e4c-c84b-478e-92e4-5f072f985971
Spec:
  Resource Policy:
    Container Policies:
      Container Name:  *
      Controlled Resources:
        cpu
        memory
      Max Allowed:
        Cpu:     1
        Memory:  500Mi
      Min Allowed:
        Cpu:     5m
        Memory:  5Mi
  Target Ref:
    API Version:  argoproj.io/v1alpha1
    Kind:         Rollout
    Name:         vpa-demo-rollout
  Update Policy:
    Update Mode:  Auto
Events:           <none>
```

After few minutes when VPA starts to process and provide recommendation:

```yaml
Name:         kubengix-vpa
Namespace:    test-vpa
Labels:       <none>
Annotations:  <none>
API Version:  autoscaling.k8s.io/v1
Kind:         VerticalPodAutoscaler
Metadata:
  Creation Timestamp:  2022-03-14T12:54:06Z
  Generation:          2
  Managed Fields:
    API Version:  autoscaling.k8s.io/v1beta2
    Fields Type:  FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          .:
          f:kubectl.kubernetes.io/last-applied-configuration:
      f:spec:
        .:
        f:resourcePolicy:
          .:
          f:containerPolicies:
        f:targetRef:
          .:
          f:apiVersion:
          f:kind:
          f:name:
        f:updatePolicy:
          .:
          f:updateMode:
    Manager:      kubectl-client-side-apply
    Operation:    Update
    Time:         2022-03-14T12:54:06Z
    API Version:  autoscaling.k8s.io/v1
    Fields Type:  FieldsV1
    fieldsV1:
      f:status:
        .:
        f:conditions:
        f:recommendation:
          .:
          f:containerRecommendations:
    Manager:         recommender
    Operation:       Update
    Time:            2022-03-14T12:54:52Z
  Resource Version:  3950
  UID:               4ac64e4c-c84b-478e-92e4-5f072f985971
Spec:
  Resource Policy:
    Container Policies:
      Container Name:  *
      Controlled Resources:
        cpu
        memory
      Max Allowed:
        Cpu:     1
        Memory:  500Mi
      Min Allowed:
        Cpu:     5m
        Memory:  5Mi
  Target Ref:
    API Version:  argoproj.io/v1alpha1
    Kind:         Rollout
    Name:         vpa-demo-rollout
  Update Policy:
    Update Mode:  Auto
Status:
  Conditions:
    Last Transition Time:  2022-03-14T12:54:52Z
    Status:                True
    Type:                  RecommendationProvided
  Recommendation:
    Container Recommendations:
      Container Name:  vpa-demo-rollout
      Lower Bound:
        Cpu:     25m
        Memory:  262144k
      Target:
        Cpu:     25m
        Memory:  262144k
      Uncapped Target:
        Cpu:     25m
        Memory:  262144k
      Upper Bound:
        Cpu:     1
        Memory:  500Mi
Events:          <none>
```

Here we see the recommendation for cpu, memory with lowerbound, upper bound, Target etc., are provided. If we check the status of the pods.. the older pods with initial configuration would get terminated and newer pods get created.

```yaml
# kubectl get po -n test-vpa -w   
NAME                               READY   STATUS    RESTARTS   AGE
vpa-demo-rollout-f5df6d577-65f26   1/1     Running   0          17m
vpa-demo-rollout-f5df6d577-d55cx   1/1     Running   0          17m
vpa-demo-rollout-f5df6d577-fdpn2   1/1     Running   0          17m
vpa-demo-rollout-f5df6d577-jg2pw   1/1     Running   0          17m
vpa-demo-rollout-f5df6d577-vlx5x   1/1     Running   0          17m
...

vpa-demo-rollout-f5df6d577-jg2pw   1/1     Terminating   0          17m
vpa-demo-rollout-f5df6d577-vlx5x   1/1     Terminating   0          17m
vpa-demo-rollout-f5df6d577-jg2pw   0/1     Terminating   0          18m
vpa-demo-rollout-f5df6d577-vlx5x   0/1     Terminating   0          18m
vpa-demo-rollout-f5df6d577-w7tx4   0/1     Pending       0          0s
vpa-demo-rollout-f5df6d577-w7tx4   0/1     Pending       0          0s
vpa-demo-rollout-f5df6d577-w7tx4   0/1     ContainerCreating   0          0s
vpa-demo-rollout-f5df6d577-vdlqq   0/1     Pending             0          0s
vpa-demo-rollout-f5df6d577-vdlqq   0/1     Pending             0          1s
vpa-demo-rollout-f5df6d577-jg2pw   0/1     Terminating         0          18m
vpa-demo-rollout-f5df6d577-jg2pw   0/1     Terminating         0          18m
vpa-demo-rollout-f5df6d577-vdlqq   0/1     ContainerCreating   0          1s
vpa-demo-rollout-f5df6d577-w7tx4   1/1     Running             0          6s
vpa-demo-rollout-f5df6d577-vdlqq   1/1     Running             0          7s
vpa-demo-rollout-f5df6d577-vlx5x   0/1     Terminating         0          18m
vpa-demo-rollout-f5df6d577-vlx5x   0/1     Terminating         0          18m
```

If we check the new pod cpu and memory they would be updated as per VPA recommendation:


```yaml
# kubectl describe po vpa-demo-rollout-f5df6d577-vdlqq -n test-vpa
Name:         vpa-demo-rollout-f5df6d577-vdlqq
Namespace:    test-vpa
Priority:     0
Node:         argo-rollouts-control-plane/172.18.0.2
Start Time:   Mon, 14 Mar 2022 12:55:06 +0000
Labels:       app=vpa-demo-rollout
              rollouts-pod-template-hash=f5df6d577
Annotations:  vpaObservedContainers: vpa-demo-rollout
              vpaUpdates: Pod resources updated by kubengix-vpa: container 0: cpu request, memory request
Status:       Running
IP:           10.244.0.17
IPs:
  IP:           10.244.0.17
Controlled By:  ReplicaSet/vpa-demo-rollout-f5df6d577
Containers:
  vpa-demo-rollout:
    Container ID:   containerd://b79bd88851fe0622d33bc90a1560ca54ef2c27405a3bc9a4fc3a333eef5f9733
    Image:          ravihari/nginx:v1
    Image ID:       docker.io/ravihari/nginx@sha256:205961b09a80476af4c2379841bf6abec0022101a7e6c5585a88316f7115d17a
    Port:           80/TCP
    Host Port:      0/TCP
    State:          Running
      Started:      Mon, 14 Mar 2022 12:55:11 +0000
    Ready:          True
    Restart Count:  0
    Requests:
      cpu:        25m
      memory:     262144k
    Environment:  <none>
    Mounts:
      /var/run/secrets/kubernetes.io/serviceaccount from kube-api-access-mk4fz (ro)
Conditions:
  Type              Status
  Initialized       True 
  Ready             True 
  ContainersReady   True 
  PodScheduled      True 
Volumes:
  kube-api-access-mk4fz:
    Type:                    Projected (a volume that contains injected data from multiple sources)
    TokenExpirationSeconds:  3607
    ConfigMapName:           kube-root-ca.crt
    ConfigMapOptional:       <nil>
    DownwardAPI:             true
QoS Class:                   Burstable
Node-Selectors:              <none>
Tolerations:                 node.kubernetes.io/not-ready:NoExecute op=Exists for 300s
                             node.kubernetes.io/unreachable:NoExecute op=Exists for 300s
Events:
  Type    Reason     Age   From               Message
  ----    ------     ----  ----               -------
  Normal  Scheduled  38s   default-scheduler  Successfully assigned test-vpa/vpa-demo-rollout-f5df6d577-vdlqq to argo-rollouts-control-plane
  Normal  Pulled     35s   kubelet            Container image "ravihari/nginx:v1" already present on machine
  Normal  Created    35s   kubelet            Created container vpa-demo-rollout
  Normal  Started    33s   kubelet            Started container vpa-demo-rollout
```
  
## Requirements  
In order for the VPA to manipulate the rollout, the Kubernetes cluster hosting the rollout CRD needs the subresources support for CRDs.  This feature was introduced as alpha in Kubernetes version 1.10 and transitioned to beta in Kubernetes version 1.11.  If a user wants to use VPA on v1.10, the Kubernetes Cluster operator will need to add a custom feature flag to the API server.  After 1.10, the flag is turned on by default.  Check out the following [link](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) for more information on setting the custom feature flag.

When installing VPA you may need to add the following in RBAC configurations for `system:vpa-target-reader` cluster role as by default VPA maynot support rollouts in all the versions.

```yaml
  - apiGroups:
      - argoproj.io
    resources:
      - rollouts
      - rollouts/scale
      - rollouts/status
      - replicasets
    verbs:
      - get
      - list
      - watch
```

Makes sure Metrics-Server is installed in the cluster and openssl is upto date for VPA latest version to apply recommendations to the pods properly. 
