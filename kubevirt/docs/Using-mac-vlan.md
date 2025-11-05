# Using macvtap binding with VLAN-backed host networks

The following guide walks through preparing your Kubernetes nodes with a VLAN-backed
macvtap lower interface and then wiring a KubeVirt virtual machine interface to that
network through the macvtap binding plugin. The examples assume VLAN ID `12`, but you can
adapt the values to match your environment.

## 1. Prepare each node with netplan

1. Identify the physical NIC that will carry the VLAN (for example `eno1`).
2. Create or extend a Netplan file on every node (for example `/etc/netplan/60-vlan12.yaml`).
   The snippet below assigns a management address to the VLAN interface and keeps DHCP on the
   untagged interface disabled so that only tagged traffic is used.

   ```yaml
   network:
     version: 2
     renderer: networkd
     ethernets:
       eno1:
         dhcp4: false
         dhcp6: false
     vlans:
       eno1.12:
         id: 12
         link: eno1
         mtu: 1500
         addresses:
           - 192.0.2.10/24  # replace with a routable management IP for this node
         routes:
           - to: 192.0.2.0/24
             via: 192.0.2.1
             on-link: true
   ```

3. Apply the configuration and verify the VLAN link is up:

   ```bash
   sudo netplan apply
   ip -d link show eno1.12
   ```

4. Repeat for every worker node that will host KubeVirt workloads. The macvtap CNI will use
   `eno1.12` as the lower device when provisioning VM tap devices.

## 2. Install the macvtap CNI and advertise resources

1. Make sure the macvtap CNI binary is present on every node (for example under
   `/opt/cni/bin/macvtap`). This binary is typically shipped with
   [k8snetworkplumbingwg/macvtap-cni](https://github.com/k8snetworkplumbingwg/macvtap-cni).
2. Deploy the macvtap device plugin DaemonSet provided by your distribution so that Kubernetes
   advertises extended resources named `macvtap.network.kubevirt.io/<lowerDevice>`. KubeVirt test
   helpers expect this annotation when building `NetworkAttachmentDefinition` objects for macvtap
   networks.【F:kubevirt/tests/libnet/netattachdef.go†L70-L75】
3. Confirm that each node now shows the resource by running:

   ```bash
   kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}:{\n}{range .status.allocatable[*]}  {.name}={.}{\n}{end}{end}' | grep macvtap.network.kubevirt.io
   ```

## 3. Enable the macvtap binding plugin in KubeVirt

KubeVirt discovers binding plugins from the `spec.configuration.network.binding` section of its
`KubeVirt` custom resource. The plugin name must be `macvtap` to match the value referenced by the
library helper that constructs macvtap interfaces.【F:kubevirt/pkg/libvmi/network.go†L87-L94】 Registering
plugins follows the same flow as the test helper `WithNetBindingPluginIfNotPresent`, which adds an entry
under `/spec/configuration/network/binding/<name>` when it is missing.【F:kubevirt/tests/libkubevirt/config/netbinding.go†L32-L53】

Create `macvtap-config/kubevirt-network-binding.yaml` with the following content and apply it in the
`kubevirt` namespace:

```yaml
apiVersion: kubevirt.io/v1
kind: KubeVirt
metadata:
  name: kubevirt
  namespace: kubevirt
spec:
  configuration:
    network:
      binding:
        macvtap:
          domainAttachmentType: tap
```

Apply the manifest:

```bash
kubectl apply -f macvtap-config/kubevirt-network-binding.yaml
```

## 4. Define NetworkAttachmentDefinition and RBAC

Create the folder `macvtap-config/` in your configuration repository and populate it with the manifests
referenced in this guide. Start with the `NetworkAttachmentDefinition` that points to the VLAN-backed
macvtap resource advertised for `eno1.12`:

```yaml
# macvtap-config/net-attach-def.yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: vlan12-macvtap
  namespace: macvtap-demo
  annotations:
    k8s.v1.cni.cncf.io/resourceName: macvtap.network.kubevirt.io/eno1.12
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "name": "vlan12-macvtap",
      "type": "macvtap"
    }
```

The annotation matches the resource name expected by KubeVirt when building macvtap network attachment definitions.【F:kubevirt/tests/libnet/netattachdef.go†L70-L75】

Grant a namespace-scoped team permission to manage the attachment definition and launch macvtap-enabled
virtual machines. Create `macvtap-config/namespace-rbac.yaml` with a dedicated namespace, service account,
role, and role binding:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: macvtap-demo
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: macvtap-launcher
  namespace: macvtap-demo
---
kind: Role
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: macvtap-admin
  namespace: macvtap-demo
rules:
  - apiGroups: ["k8s.cni.cncf.io"]
    resources: ["network-attachment-definitions"]
    verbs: ["get", "list", "create", "update", "patch", "watch"]
  - apiGroups: ["kubevirt.io"]
    resources: ["virtualmachines", "virtualmachineinstances"]
    verbs: ["get", "list", "create", "update", "patch", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: macvtap-admin-binding
  namespace: macvtap-demo
subjects:
  - kind: ServiceAccount
    name: macvtap-launcher
    namespace: macvtap-demo
roleRef:
  kind: Role
  name: macvtap-admin
  apiGroup: rbac.authorization.k8s.io
```

Apply both manifests:

```bash
kubectl apply -f macvtap-config/namespace-rbac.yaml
kubectl apply -f macvtap-config/net-attach-def.yaml
```

## 5. Launch a macvtap-enabled virtual machine

With the binding plugin registered and the network attachment defined, create a virtual machine that
requests the macvtap interface and references the Multus network. The `macvtap` binding name in the
interface spec must match the plugin registration.【F:kubevirt/pkg/libvmi/network.go†L87-L94】 The example
below can be stored as `macvtap-config/vm-alpine.yaml`:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: macvtap-alpine
  namespace: macvtap-demo
spec:
  running: true
  template:
    spec:
      domain:
        devices:
          interfaces:
            - name: vlan12
              binding:
                name: macvtap
              macAddress: "02:00:ca:fe:00:12"
      networks:
        - name: vlan12
          multus:
            networkName: macvtap-demo/vlan12-macvtap
      volumes:
        - name: macvtap-launcher-sa
          serviceAccount:
            serviceAccountName: macvtap-launcher
```

KubeVirt will build the network attachment using the macvtap plugin and rely on the Multus annotation to
locate the `vlan12-macvtap` definition.【F:kubevirt/docs/network/network-binding-plugin.md†L68-L120】【F:kubevirt/pkg/libvmi/network.go†L87-L110】
The `serviceAccount` volume makes the resulting virt-launcher pod run with the `macvtap-launcher`
permissions that were granted earlier; KubeVirt derives the pod service account from this volume when it
renders the launcher manifest.【F:kubevirt/pkg/virt-controller/services/template.go†L643-L656】【F:kubevirt/pkg/virt-controller/services/rendervolumes.go†L578-L583】 Add any other disks or volumes your
workload requires alongside the service account volume.

Create the VM and confirm that the interface appears inside the guest:

```bash
kubectl apply -f macvtap-config/vm-alpine.yaml
kubectl wait vmi/macvtap-alpine -n macvtap-demo --for=condition=Ready --timeout=5m
kubectl console vmi/macvtap-alpine -n macvtap-demo
```

Inside the guest, configure IP addressing appropriate for VLAN 12 and verify connectivity.

## 6. Troubleshooting checklist

- Confirm that every node hosting VMs exposes the expected `macvtap.network.kubevirt.io/eno1.12` resource.
- Ensure the VLAN interface is operational on the host (`ip addr show eno1.12`).
- Check that the KubeVirt CR reports the macvtap binding under `spec.configuration.network.binding`.
- Use `kubectl describe network-attachment-definition vlan12-macvtap -n macvtap-demo` to verify the
  annotation and plugin type values.
- Inspect the virt-launcher pod logs for macvtap CNI messages if interface creation fails.

By following these steps you can connect KubeVirt guests directly to a VLAN-backed L2 segment using the
macvtap binding plugin, while keeping manifests organized under the `macvtap-config/` folder for easy reuse.
