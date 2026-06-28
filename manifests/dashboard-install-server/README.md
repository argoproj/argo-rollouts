# Argo Rollouts Dashboard — authenticated server mode

Deploys the dashboard with authentication, RBAC, and TLS enabled
(`--auth-mode=server`). The default `manifests/dashboard-install` runs the
dashboard with NO authentication and is unchanged.

## Before you apply

1. Set a signing key (the server refuses to start without one ≥32 bytes):

       kubectl -n <ns> create secret generic argo-rollouts-dashboard-secret \
         --from-literal=server.secretkey="$(openssl rand -base64 48)" \
         --dry-run=client -o yaml | kubectl apply -f -

   Or edit `dashboard-secret.yaml` before applying.

2. Set an admin password (bcrypt) in the same secret under `admin.password`,
   or configure OIDC in `argo-rollouts-dashboard-cm` (`oidc.config`) and set
   `url` to the dashboard's external address.

3. Review `argo-rollouts-dashboard-rbac-cm` — `policy.default: role:readonly`
   grants every authenticated user read access; tighten as needed.

## Apply

    kubectl apply -k manifests/dashboard-install-server     # or
    kubectl apply -f manifests/dashboard-install-server.yaml

## Notes

- TLS: a self-signed cert is generated if `tls.crt`/`tls.key` are not set in
  the secret. Use `--insecure` (edit the deployment args) to disable TLS when
  terminating it at an ingress.
- The dashboard ServiceAccount is granted `get` on exactly its three config
  objects (least privilege) in addition to the rollouts access from the base.
