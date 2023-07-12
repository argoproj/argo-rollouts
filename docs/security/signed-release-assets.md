# Verification of Argo Rollouts Artifacts

## Prerequisites
- cosign `v2.0.0` or higher [installation instructions](https://docs.sigstore.dev/cosign/installation)
- slsa-verifier [installation instructions](https://github.com/slsa-framework/slsa-verifier#installation)

***
## Release Assets
| Asset                               | Description                                      |
|-------------------------------------|--------------------------------------------------|
| argo-rollouts-checksums.txt         | Checksums of binaries                            |
| argo-rollouts-cli.intoto.jsonl      | Attestation of CLI binaries & manifiest          |
| dashboard-install.yaml              | Dashboard install                                |
| install.yaml                        | Standard installation method                     |
| kubectl-argo-rollouts-darwin-amd64  | CLI Binary                                       |
| kubectl-argo-rollouts-darwin-arm64  | CLI Binary                                       |
| kubectl-argo-rollouts-linux-amd64   | CLI Binary                                       |
| kubectl-argo-rollouts-linux-arm64   | CLI Binary                                       |
| kubectl-argo-rollouts-windows-amd64 | CLI Binary                                       |
| namespace-install.yaml              | Namespace installation                           |
| notifications-install.yaml          | Notification installation                        |
| rollout_cr_schema.json              | Schema                                           |
| sbom.tar.gz                         | Sbom                                             |
| sbom.tar.gz.pem                     | Certificate used to sign sbom                    |
| sbom.tar.gz.sig                     | Signature of sbom                                |

***
## Verification of container images

Argo Rollouts container images are signed by [cosign](https://github.com/sigstore/cosign) using identity-based ("keyless") signing and transparency. Executing the following command can be used to verify the signature of a container image:

```bash
cosign verify \
--certificate-identity-regexp https://github.com/argoproj/argo-rollouts/.github/workflows/image-reuse.yaml@refs/tags/v \
--certificate-oidc-issuer https://token.actions.githubusercontent.com \
quay.io/argoproj/argo-rollouts:v1.5.0 | jq
```
The command should output the following if the container image was correctly verified:
```bash
The following checks were performed on each of these signatures:
  - The cosign claims were validated
  - Existence of the claims in the transparency log was verified offline
  - The code-signing certificate was verified using trusted certificate authority certificates
[
  {
    "critical": {
      "identity": {
        "docker-reference": "quay.io/argoproj/argo-rollouts"
      },
      "image": {
        "docker-manifest-digest": "sha256:519522f8c66c7b4c468f360ebe6c8ba07b8d64f5f948e71ae52c01b9953e1eb9"
      },
      "type": "cosign container image signature"
    },
    "optional": {
      "1.3.6.1.4.1.57264.1.1": "https://token.actions.githubusercontent.com",
      "1.3.6.1.4.1.57264.1.2": "push",
      "1.3.6.1.4.1.57264.1.3": "aa1afcb418fcebcc68b063377c48225f5a9d1511",
      "1.3.6.1.4.1.57264.1.4": "Release",
      "1.3.6.1.4.1.57264.1.5": "argoproj/argo-rollouts",
      "1.3.6.1.4.1.57264.1.6": "refs/tags/v1.5.0",
      ...
```

***
## Verification of container image attestations

A [SLSA](https://slsa.dev/) Level 3 provenance is generated using [slsa-github-generator](https://github.com/slsa-framework/slsa-github-generator).

The following command will verify the signature of an attestation and how it was issued. It will contain the payloadType, payload, and signature.
```bash
cosign verify-attestation --type slsaprovenance \
--certificate-identity-regexp https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/v \
--certificate-oidc-issuer https://token.actions.githubusercontent.com \
quay.io/argoproj/argo-rollouts:v1.5.0 | jq
```
The payload is a non-falsifiable provenance which is base64 encoded and can be viewed by using the command below:
```bash
cosign verify-attestation --type slsaprovenance \
--certificate-identity-regexp https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/v \
--certificate-oidc-issuer https://token.actions.githubusercontent.com \
quay.io/argoproj/argo-rollouts:v1.5.0 | jq -r .payload | base64 -d | jq
```
!!! tip
    `cosign` or `slsa-verifier` can both be used to verify image attestations.
    Check the documentation of each binary for detailed instructions.

***
## Verification of CLI artifacts with attestations

A single attestation (`argo-rollouts.intoto.jsonl`) from each release is provided. This can be used with [slsa-verifier](https://github.com/slsa-framework/slsa-verifier#verification-for-github-builders) to verify that a CLI binary or manifest was generated using Argo Rollouts workflows on GitHub and ensures it was cryptographically signed.
```bash
slsa-verifier verify-artifact kubectl-argo-rollouts-linux-amd64 --provenance-path kubectl-argo-rollouts.intoto.jsonl  --source-uri github.com/argoproj/argo-rollouts
```
## Verifying an artifact and output the provenance

```bash
slsa-verifier verify-artifact kubectl-argo-rollouts-linux-amd64 --provenance-path kubectl-argo-rollouts.intoto.jsonl  --source-uri github.com/argoproj/argo-rollouts --print-provenance | jq
```
## Verification of Sbom

```bash
cosign verify-blob --signature sbom.tar.gz.sig --certificate sbom.tar.gz.pem \
--certificate-identity-regexp ^https://github.com/argoproj/argo-rollouts/.github/workflows/release.yaml@refs/tags/v \
--certificate-oidc-issuer https://token.actions.githubusercontent.com  \
 ~/Downloads/sbom.tar.gz | jq
```

***
## Verification on Kubernetes

### Policy controllers
!!! note
    We encourage all users to verify signatures and provenances with your admission/policy controller of choice. Doing so will verify that an image was built by us before it's deployed on your Kubernetes cluster.

Cosign signatures and SLSA provenances are compatible with several types of admission controllers. Please see the [cosign documentation](https://docs.sigstore.dev/cosign/overview/#kubernetes-integrations) and [slsa-github-generator](https://github.com/slsa-framework/slsa-github-generator/blob/main/internal/builders/container/README.md#verification) for supported controllers.
