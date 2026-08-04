# Horizontal Pod Autoscaling
Argo Rollouts supports autoscaling using the standard Kubernetes Horizontal Pod Autoscaler (HPA) to manage the number of pods during a progressive rollout based on application load.

## HPA Support in Argo Rollouts
Argo Rollout works with the Horizontal Pod Autoscaler (HPA) through `autoscaling/v2beta` or higher and the stable `autoscaling/v2` APIs (available in Kubernetes 1.23+). These APIs provide the functionality for the HPA to target and scale custom resources, including the Argo Rollout object.

## How HPA Works with Argo Rollouts

Since version v0.3.0, Argo Rollouts exposes its `/scale` subresource in the same way as a standard Kubernetes `Deployment` does. This allows the Horizontal Pod Autoscaler (HPA) to discover the Rollout resource. The HPA accesses the /scale subresource to get the current number of replicas from the `status.replicas` field of the Rollout. 

Based on metrics the HPA monitors (e.g., CPU, memory, or custom metrics), the HPA decides if scaling is needed. If scaling is needed, the HPA writes the new desired replica count to the `spec.replicas` field of the Rollout via the same `/scale` subresource. 

The HPA does not directly manage the number of replicas or ReplicaSets. Instead, it modifies the `spec.replicas` field of the Rollout resource to set the desired number of replicas. The Argo Rollouts controller then detects the changes to the `spec.replicas` field and, when it sees the replica count updated by the HPA, instructs the ReplicaSet to create or delete pods.

In short, the HPA makes scaling decisions by determining the total desired number of pods, while Argo Rollouts carries out the deployment changes, allocating pods according to the configured rollout strategy and properties.

### Example Configuration:
The following YAML provides a base configuration that includes a Rollout, two Services (for blue/green or canary), and an HPA for autoscaling. Adapt this configuration for different deployment strategies and scenarios as explained in the subsequent sections.

### 1. Rollout
The core Argo Rollout object defines deployment strategy (e.g., blue/green or canary) and replaces the Kubernetes Deployment. The HPA targets this object to manage the total desired replica count.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-hpa-example
spec:
  replicas: 3	# HPA will scale this between min/max replicas
  selector:
    matchLabels:
    app: demo
  template:
    metadata:
      labels:
        app: demo
    spec:
      containers:
      - name: demo
        image: https://github.com/argoproj/rollouts-demo
        ports:
        - containerPort: 8080
  strategy:
    blueGreen | canary: # Replace with either blueGreen or canary
      autoPromotionEnabled: false
```
### 2. Service
Two Kubernetes `Service`s are required: a stable service for live traffic and a preview service to test the new version before a full promotion.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: argo-rollouts-stable-service
spec:
  ports:
  - port: 80
    targetPort: 8080
  selector:
    app: demo
---
apiVersion: v1
kind: Service
metadata:
  name: argo-rollouts-preview-service
spec:
  ports:
  - port: 80
    targetPort: 8080
  selector:
    app: demo
```
### 3. HorizontalPodAutoscaler
The HPA targets a Rollout for scaling, monitors its pods' average metrics, and adjusts the total desired pod count.
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: demo-hpa
  labels:
    app: demo
spec:      # The min and max number of pods the HPA can scale
  minReplicas: 1
  maxReplicas: 10
  scaleTargetRef:		# The HPA targets the Rollout object for scaling.
    apiVersion: argoproj.io/v1alpha1
    kind: Rollout
    name: rollout-hpa-example
  metrics: # Defines the scaling trigger
  - type: Resource
    resource:
      name: memory
      target:
        type: AverageValue
        averageValue: 16Mi
```

## Blue/Green Deployments with HPA

During a Blue/Green deployment, the Rollout controller manages two ReplicaSets: an active (`Blue`) for the current version and a preview (`Green`) one for the new version. When an HPA scales the deployment up or down, it updates the total desired replica count in the Rollout's `spec.replicas` field. The Rollout controller detects this change and applies it to both the active and preview ReplicaSets. As a result, both blue and green versions scale in unison, maintaining equal pod counts.

After full promotion, the preview ReplicaSet becomes the new active ReplicaSet, and the old active ReplicaSet scales to zero. From then on, the Rollout manages only the single active ReplicaSet, and any further HPA scaling adjustments will be applied only to this active ReplicaSet.

To implement a Blue/Green deployment, add the following `strategy` configuration to the base Rollout resource:

```yaml
strategy:
  blueGreen:
    previewService: argo-rollouts-preview-service
    activeService: argo-rollouts-stable-service
    autoPromotionEnabled: false
```
**Important**: Running both Blue and Green environments simultaneously doubles resource usage and costs. Autoscaling can incur additional costs on top of this. To minimize preview costs during extended testing periods, use the `previewReplicaCount` property described in the next section.

## Blue/Green with **`previewReplicaCount`**

When using the `previewReplicaCount` field in the Blue/Green strategy, the Rollout controller will change the stable ReplicaSet count as instructed by the HPA, while keeping the preview ReplicaSet pinned to the number of pods specified in the `previewReplicaCount` field in the Rollout manifest.

After rollout is fully promoted, the preview version becomes the new stable ReplicaSet. At that point, the HPA can manage the Rollout to scale up or down the number of pods in the stable version.

```yaml
strategy:
  blueGreen:
    previewService: argo-rollouts-preview-service
    activeService: argo-rollouts-stable-service
    previewReplicaCount: 1	   # Pins the number of pods in the preview
    autoPromotionEnabled: false
```
The `previewReplicaCount` prevents the HPA from scaling the preview ReplicaSet, keeping it fixed during testing. Only the stable ReplicaSet responds to HPA scaling decisions. Skipping this `previewReplicaCount` in the configuration allows HPA to scale both stable and preview ReplicaSets in unison, which incurs additional resource usage and cost.

**Warning:** Use `previewReplicaCount` with caution, as it always takes precedence regardless of the current load or HPA scaling decisions. 

## Canary Deployments with HPA
Unlike a Blue/Green deployment, autoscaling a Canary deployment is more complex because traffic is split between two active versions: canary and stable.

## Canary without Traffic Manager (**`setWeight`** only)
Without a traffic manager, Kubernetes Services distribute traffic evenly across all available pods, and the Rollout controller uses the pod count to split traffic between the preview (canary) and stable versions. As a result, 20% canary pods receive approximately 20% of the traffic.

As the application load changes continuously, the HPA scales the total number of pods up or down. During promotion, when the `setWeight` increases (e.g., 20% -> 50%), the Rollout controller decreases the stable pods and increases the canary pods to match the new ratio.

The HPA monitors a set of pods through a label selector (e.g., `app: demo`) to identify which pods to monitor. Since the pods for both the stable and canary ReplicaSets share this label, the HPA sees them as a single group and calculates the average metric value across all of these pods combined. 

The HPA calculates the desired number of replicas using this formula:
```  
  desired_replicas = ⌈ current_replicas x ( current_metric_value / desired_metric_value) ⌉
```
*The formula above shows a ceiling function, which always rounds up any fraction to the next whole number.*

The Rollout controller then decides how the total desired replicas should be distributed based on the `setWeight` defined in the Rollout strategy. It calculates the number of canary and stable pods using this formula:
```
	canary_replicas = ⌈ HPA_total_desired_replicas x (setWeight / 100)⌉

	stable_replicas = HPA_total_desired_replicas - canary_replicas
```
#### Example with `setWeight: 20`

When HPA decides that 10 pods are needed to handle the current load, the Rollout controller sets:
```
Canary Pods: ceil(10 * 0.2) = 2 pods
Stable Pods: 10 - 2 = 8 pods
```
If the traffic suddenly spikes and HPA scales to 20 pods, the Rollout controller instantly adjusts to:
```
Canary Pods: ceil( 20 * 0.2) = 4 pods
Stable Pods: 20 - 4 = 16 pods
```
The `setWeight` property is defined within the `strategy.canary.steps` field of the `Rollout` manifest.
```yaml
strategy:
  canary:
    canaryService: canary-service
    stableService: stable-service 
    steps:
    - setWeight: 20
    - pause: {}
    - setWeight: 50
    - pause: {}
    - setWeight: 100
    - pause: {}
```
**Note:** If the rollout is aborted and rolled back, the stable pods will need to be scaled back up manually.

## Canary with Traffic Manager
When using a traffic manager (e.g., Traefik, Istio, Ingress etc), the responsibility for traffic splitting shifts from Argo Rollouts to the traffic manager. Instead of adjusting pod counts, the Rollouts controller updates the traffic manager’s configuration at each `setWeight` step, specifying what percentage of traffic should go to the new canary version.

At each `setWeight` step, the controller edits the traffic manager’s custom configuration resource (e.g., a `TraefikService` for Traefik or a `VirtualService` for Istio) to update the `weight` field. The traffic manager detects this configuration change and immediately begins routing the specified percentage of traffic to the canary service, while the remaining traffic continues flowing to the stable service. 

For example, `setWeight: 20` results in 20% of traffic going to the canary and 80% to the stable version, no matter how many pods are running for each version.

The key difference from the default behavior (without a traffic manager) is that Argo Rollouts would achieve a 20% traffic shift by adjusting pod counts to a 20/80 ratio. With a traffic manager, traffic is split independently of pod counts. To manage both traffic distribution and pod counts, see the next section.

**Warning: Lack of Scaling Isolation**<br>
Using a single HPA for both stable and canary pods has a drawback: scaling isn’t decoupled between the stable and canary versions. If the canary has a performance issue (e.g. a memory leak or CPU spike), the HPA will see a high average metric across all pods and scale up the entire application. This means the stable version also gets scaled up due to a problem in the canary. Therefore, it is crucial to develop the applications free of memory leaks and performance issues for smooth canary releases.

### Example Configuration: 
In this example, Traefik is used as the traffic manager. The setup requires: an `IngressRoute` for Traefik to expose the service, a `TraefikService` to handle the weighted load balancing between canary and stable, and a `Rollout` configured to use Traefik's `trafficRouting`. 
The HPA still targets the Rollouts resource as in previous examples.

### 1. IngressRoute
The IngressRoute resource exposes an application to the outside world through Traefik and forwards incoming traffic to the `TraefikService`.

```yaml
# ingress-route.yaml
apiVersion: traefik.containo.us/v1alpha1
kind: IngressRoute
metadata:
  name: demo-ingress
  namespace: default
spec:
  entryPoints:
    - web
  routes:
  - match: PathPrefix(`/`)
    kind: Rule
    services:
    - name: traefik-service	# Points to the TraefikService
      namespace: default
      kind: TraefikService
```
### 2. TraefikService
The custom resource `TraefikService` defines how Traefik distributes traffic between two Kubernetes `Services` (e.g., stable and canary).

``` YAML
# traefik-service.yaml
apiVersion: traefik.containo.us/v1alpha1
kind: TraefikService
metadata:
  name: traefik-service
spec:
  weighted:
    services:
      - name: rollout-canary-stable
        port: 80
      - name: rollout-canary-preview 
        port: 80
```
### 3. Rollout with Traefik `trafficRouting`
Include the `trafficRouting` property in the `Rollout` manifest. The `trafficRouting.traefik.name` must match the `TraefikService` name to route traffic to the Rollout strategy.

```yaml
strategy:
  canary:
    canaryService: canary-service 
    stableService: stable-service 
    trafficRouting: # Add trafficRouting in the Rollout manifest
      traefik: 	    
        weightedTraefikServiceName: traefik-service # This name MUST match 	 
    steps: 
    - setWeight: 20 
    - pause: {}
```
## Decoupling Canary with Traffic Manager (`setCanaryScale`)
The `setCanaryScale` field decouples canary scaling from the HPA by pinning the canary ReplicaSet to a fixed number of pods at each rollout step. The HPA continues to manage the total replica count, while the Rollout controller ensures the canary pods number remains fixed, so the HPA scaling is applied only to the stable pods by calculating the number of stable pods as:
```
stable_replicas = HPA_total_desired_replicas - pinned_canary_replicas
```
The Rollout controller ensures the stable ReplicaSet scales dynamically with the HPA based on metrics, while the canary ReplicaSet runs with a fixed number of pods and receives traffic according to `setWeight`. It prevents the canary from consuming excessive resources or triggering unnecessary scaling of the stable environment due to faulty canary behavior during tests.

This strategy decouples traffic weight from pod counts:

With `setWeight: 20` and `setCanaryScale.replicas: 1`, the traffic manager will send 20% of traffic to the single pinned canary pod, while the remaining 80% of traffic goes to the stable service (backed by all the autoscaled stable pods). 

With `setWeight: 80` and `setCanaryScale.replicas: 1`, the traffic manager sends 80% of total traffic to that single pinned canary pod, no matter how many stable pods are running. 

It allows you to both control traffic and keep the canary resource usage minimal.

```yaml
strategy:
  canary:
    canaryService: canary-service 
    stableService: stable-service 
    trafficRouting: 
      traefik: 
        weightedTraefikServiceName: traefik-service 
    steps: 
    - setWeight: 20 
    - setCanaryScale: replicas: 1 	# one canary pod receives 20% traffic
    - pause: {} 
    - setWeight: 50 
    - setCanaryScale: replicas: 3 	# three canary pods receive 50% traffic
    - pause: {} 
    - setWeight: 90 
    - setCanaryScale: replicas: 5 	# five canary pods receive 90% traffic
    - pause: {} 
    - setWeight: 100 
    - setCanaryScale: replicas: 8 	# eight canary pods receive 100% traffic
    - pause: {}
```


## Best Practices
1. Choose the right strategy: use standard Blue/Green for simple deployments, add `previewReplicaCount` for cost optimization, and consider canary with `setCanaryScale` for maximum control and isolation.
2. Monitor both versions during deployments: make sure your monitoring covers both stable and canary/preview versions to detect any performance anomalies early.
3. Set appropriate HPA thresholds: configure your HPA min/max replicas and target metrics to align with your application’s specific performance characteristics.
4. Test rollback scenarios: in some canary scenarios, manual scaling back is required
5. Implement proper resource requests and limits: set appropriate resource requests and limits on your pods to help the HPA make accurate scaling decisions.
6. Use traffic managers for controlled deployments rather than relying on pod ratios alone.