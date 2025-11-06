# Macvtap admission webhook

This module implements a mutating admission webhook that automatically injects
the Linux capabilities required by the macvtap binding plugin into Pods. Run it
alongside KubeVirt to guarantee that any Pod requesting macvtap resources gets
`securityContext.privileged: true` together with the `DAC_OVERRIDE`,
`NET_ADMIN`, and `SYS_RAWIO` capabilities.

## Understand what the webhook mutates

1. The webhook inspects every Pod admission request.
2. It looks for any of the following macvtap signals:
   * Multus namespace annotations (`k8s.v1.cni.cncf.io/*`) that mention
     `macvtap`.
   * Container resource requests or limits that include `macvtap` (for example
     `macvtap.network.kubevirt.io/eno1`).
3. When it finds a match, it returns a JSON merge patch that updates **all**
   init containers and app containers to run privileged and to add the
   capabilities listed above.

## Run the webhook locally

Follow these steps from inside the `webhook-sidecard/` directory:

1. **Build the binary**

   ```bash
   go build
   ```

2. **Start the server**

   ```bash
   ./webhook-sidecard --allow-http --addr :8080
   ```

   Use `--allow-http` only for local testing. In production provide TLS
   certificates instead of enabling HTTP.

## Build the container image

1. Make sure you are still in `webhook-sidecard/`.
2. Run the build command with your preferred registry reference:

   ```bash
   docker build -t <your-registry>/webhook-sidecard:latest .
   ```

## Deploy to a cluster

1. **Review the manifests** in [`config/`](config/):
   * `service.yaml` exposes the webhook inside the cluster.
   * `deployment.yaml` runs the webhook server.
   * `mutatingwebhook.yaml` registers the webhook with the Kubernetes API
     server.
2. **Update the image** value in `deployment.yaml` to point to your published
   container.
3. **Patch the CA bundle** in `mutatingwebhook.yaml` with the base64 encoded
   certificate that signs your TLS secret.
4. **Apply the resources** in the correct order:

   ```bash
   kubectl apply -f config/service.yaml
   kubectl apply -f config/deployment.yaml
   kubectl apply -f config/mutatingwebhook.yaml
   ```

## Configure TLS

You can pass the TLS material via flags or environment variables when creating
the deployment:

* `--tls-cert` / `TLS_CERT_FILE` – Path to the serving certificate.
* `--tls-key` / `TLS_KEY_FILE` – Path to the private key.

If neither option is provided the server refuses to start unless `--allow-http`
is explicitly set (only recommended for local testing).
