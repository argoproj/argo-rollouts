# Using Argo Rollouts with Helm

Argo Rollouts will always respond to changes in Rollouts resources regardless of how the change was made.
This means that Argo Rollouts is compatible with all templating solutions that you might use to manage
your deployments.

Argo Rollouts manifests can be managed with the [Helm package manager](https://helm.sh/). If your Helm chart contains Rollout Resources,
then as soon as you install/upgrade your chart, Argo Rollouts will take over and initiate the progressive delivery
process.

Here is an example Rollout that is managed with Helm:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: {{ template "helm-guestbook.fullname" . }}
  labels:
    app: {{ template "helm-guestbook.name" . }}
    chart: {{ template "helm-guestbook.chart" . }}
    release: {{ .Release.Name }}
    heritage: {{ .Release.Service }}
spec:
  replicas: {{ .Values.replicaCount }}
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app: {{ template "helm-guestbook.name" . }}
      release: {{ .Release.Name }}
  strategy:
    blueGreen:
      activeService: {{ template "helm-guestbook.fullname" . }}
      previewService: {{ template "helm-guestbook.fullname" . }}-preview
  template:
    metadata:
      labels:
        app: {{ template "helm-guestbook.name" . }}
        release: {{ .Release.Name }}
    spec:
      containers:
        - name: {{ .Chart.Name }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: http
              containerPort: 80
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /
              port: http
          readinessProbe:
            httpGet:
              path: /
              port: http
          resources:
{{ toYaml .Values.resources | indent 12 }}
    {{- with .Values.nodeSelector }}
      nodeSelector:
{{ toYaml . | indent 8 }}
    {{- end }}
    {{- with .Values.affinity }}
      affinity:
{{ toYaml . | indent 8 }}
    {{- end }}
    {{- with .Values.tolerations }}
      tolerations:
{{ toYaml . | indent 8 }}
    {{- end }}

```


You can find the full example at [https://github.com/argoproj/argo-rollouts/tree/master/examples/helm-blue-green](https://github.com/argoproj/argo-rollouts/tree/master/examples/helm-blue-green).
