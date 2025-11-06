# webhook-sidecard

This module provides a lightweight mutating admission webhook that injects the
Linux capabilities required by the macvtap binding plugin into Pods. It is
designed to run alongside KubeVirt and automatically updates any Pod that
requests macvtap resources so that it runs in privileged mode with the
`DAC_OVERRIDE`, `NET_ADMIN`, and `SYS_RAWIO` capabilities.

## How it works

The webhook inspects Pod admission requests and looks for signs that the
workload needs the macvtap binding plugin:

* Namespace annotations created by Multus (`k8s.v1.cni.cncf.io/*`) that mention
  `macvtap`.
* Resource requests or limits with names that include `macvtap` (for example
  `macvtap.network.kubevirt.io/eno1`).

Whenever either condition is true, the webhook returns a JSON merge patch that
updates **all** init containers and app containers to include:

* `securityContext.privileged: true`
* `securityContext.capabilities.add`: `DAC_OVERRIDE`, `NET_ADMIN`, `SYS_RAWIO`

## Running locally

```bash
go build
./webhook-sidecard --allow-http --addr :8080
```

The `--allow-http` flag starts the server without TLS which is useful for local
experimentation. In production you should mount a TLS certificate and key and
omit that flag.

## Deploying to a cluster

The [`config/`](config/) directory contains example manifests:

* `deployment.yaml` – Runs the webhook server.
* `service.yaml` – Exposes the webhook inside the cluster.
* `mutatingwebhook.yaml` – Registers the webhook with the Kubernetes API
  server.

Update the image reference in the deployment and patch the certificate bundle
in the webhook configuration before applying it to your cluster.

```bash
kubectl apply -f config/service.yaml
kubectl apply -f config/deployment.yaml
kubectl apply -f config/mutatingwebhook.yaml
```

## Building the container image

```bash
# from the webhook-sidecard directory
docker build -t <your-registry>/webhook-sidecard:latest .
```

## Environment variables

* `TLS_CERT_FILE`, `TLS_KEY_FILE` – Optional alternatives to the `--tls-cert`
  and `--tls-key` flags. When neither flag nor environment variable is
  provided the server refuses to start unless `--allow-http` is used.
