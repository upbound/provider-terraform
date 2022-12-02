## Migration Guide from Community to Official Terraform Provider

This document describes the steps that need to be applied to migrate from
community provider-terraform to official Upbound provider-terraform.

For the sake of simplicity, we only focus on migrating managed resources
and compositions in this guide. These scenarios can be extended
with other tools like ArgoCD, Flux, Helm, Kustomize, etc.

### Migrating Managed Resources

Migrating existing managed resources to official providers can be simplified
as import scenarios. The aim is to modify the community provider's scheme to official
providers and apply those manifests to import existing cloud resources.

To prevent a conflict between two provider controllers reconciling for the same external resource,
we're scaling down the old provider.


1) Backup Workspace managed resource manifests
```bash
kubectl get workspaces.tf.crossplane.io -o yaml > backup-mrs.yaml
```
2) Update deletion policy to `Orphan` with the command below:
```bash
kubectl patch -f backup-mrs.yaml -p '{"spec": {"deletionPolicy":"Orphan"}}' --type=merge
```
3) Install the official provider following instructions at https://marketplace.upbound.io/providers/upbound/provider-terraform

4) Install ProviderConfig for the official provider.

Make sure that it matches the original ProviderConfig, especially the backend configuration.
If you are using `kubernetes` backend, the `secret_suffix` and `namespace` should
be identical between old and new ProviderConfigs.

The safest way to do it is to dump old ProviderConfigs and change the API group:
```bash
kubectl get providerconfigs.tf.crossplane.io -o yaml > backup-providerconfigs.yaml
cp backup-providerconfigs.yaml op-providerconfigs.yaml
```
Change the API group and remove the runtime fields

```diff
diff -u backup-providerconfigs.yaml op-providerconfigs.yaml
--- backup-providerconfigs.yaml	2022-12-01 11:12:33.000000000 +0400
+++ op-providerconfigs.yaml	2022-12-01 11:13:49.000000000 +0400
@@ -1,18 +1,9 @@
 apiVersion: v1
 items:
-- apiVersion: tf.crossplane.io/v1alpha1
+- apiVersion: tf.upbound.io/v1beta1
   kind: ProviderConfig
   metadata:
-    annotations:
-      kubectl.kubernetes.io/last-applied-configuration: |
-        {"apiVersion":"tf.crossplane.io/v1alpha1","kind":"ProviderConfig","metadata":{"annotations":{},"name":"default"},"spec":{"configuration":"terraform {\n  backend \"kubernetes\" {\n    secret_suffix     = \"providerconfig-terraform-aws\"\n    namespace         = \"upbound-system\"\n    #in_cluster_config = true\n  }\n}\nprovider \"aws\" {\n  shared_credentials_file = \"aws-creds.ini\"\n  region = \"eu-central-1\"\n}\n","credentials":[{"filename":".git-credentials","secretRef":{"key":".git-credentials","name":"git-credentials","namespace":"upbound-system"},"source":"Secret"},{"filename":"aws-creds.ini","secretRef":{"key":"credentials","name":"aws-creds","namespace":"upbound-system"},"source":"Secret"}]}}
-    creationTimestamp: "2022-12-01T07:04:28Z"
-    finalizers:
-    - in-use.crossplane.io
-    generation: 1
     name: default
-    resourceVersion: "8022900"
-    uid: 1ddf5106-245b-4f23-bc9d-30a2d76453c3
   spec:
     configuration: |
       terraform {
@@ -40,7 +31,6 @@
         namespace: upbound-system
       source: Secret
     pluginCache: true
-  status: {}
 kind: List
 metadata:
   resourceVersion: ""
```

```bash
kubectl apply -f op-providerconfig.yaml
providerconfig.tf.upbound.io/default created
```

5) Pause community provider following https://crossplane.io/docs/v1.10/reference/troubleshoot.html#pausing-providers
```bash
kubectl get providers crossplane-provider-terraform -o yaml > community-provider-terraform.yaml
cp community-provider-terraform.yaml community-provider-terraform-paused.yaml
```
Add `ControllerConfig` and `controllerConfigRef` to `Provider` spec.
Apply it and observe that the community provider deploymet was scaled down.
```bash
kubectl apply -f community-provider-terraform-paused.yaml
kubectl -n upbound-system get deploy|grep crossplane-provider-terraform
crossplane-provider-terraform-e56e83bb443a      0/0     0            0           24m
```

6) If the Workspace resources are instantiated as a part of Composition, pause
the associated Claim or XR using the
https://crossplane.io/docs/v1.10/reference/composition.html#pause-annotation,
e.g.

```bash
kubectl annotate terraformclaim.example.upbound.io/iam-role-demo-001 crossplane.io/paused=true
```

7) Update managed resource manifests to the new API version `tf.upbound.io/v1beta1`.
```bash
cp backup-mrs.yaml op-mrs.yaml
vi op-mrs.yaml
```
```diff
diff -u backup-mrs.yaml op-mrs.yaml
--- backup-mrs.yaml	2022-11-29 16:32:09.000000000 +0400
+++ op-mrs.yaml	2022-11-29 17:38:41.000000000 +0400
@@ -1,6 +1,6 @@
 apiVersion: v1
 items:
-- apiVersion: tf.crossplane.io/v1alpha1
+- apiVersion: tf.upbound.io/v1beta1
   kind: Workspace
   metadata:
     annotations:
```

8) Apply updated managed resources and wait until they become ready
```bash
kubectl apply -f op-mrs.yaml
```
If MR was using connection secret you will hit error similar to
```
  Warning  CannotPublishConnectionDetails  2s (x3 over 18s)  managed/workspace.tf.upbound.io  cannot create or update connection secret: existing secret is not controlled by UID "e8965abe-a43b-4fed-96e2-33f259644102"
```

In this case you can remove `ownerReferences` of conflicting secret, e.g.

```
kubectl edit -n crossplane-system secret 5e840aa4-e736-49a8-9a43-86fada8f3862-secret
```

Remove
```
  ownerReferences:
  - apiVersion: tf.crossplane.io/v1alpha1
    controller: true
    kind: Workspace
    name: iam-role-demo-001-tqkm2-68fff
```

9) Delete old MRs
```bash
kubectl delete -f backup-mrs.yaml
kubectl patch -f backup-mrs.yaml -p '{"metadata":{"finalizers":[]}}' --type=merge
```
10) Delete old provider configs
```bash
kubectl delete -f backup-providerconfigs.yaml
kubectl patch -f backup-providerconfigs.yaml  -p '{"metadata":{"finalizers":[]}}' --type=merge
```
11) Delete old provider
```bash
kubectl delete providers crossplane-provider-terraform
```

If you were migrating plain Workspace Managed Resources you can stop here.

If Workspaces were part of the Composition/Configuration, proceed to the next
section.

### Migrating Crossplane Configurations

Configuration migration can be more challenging. Because, in addition to managed resource migration, we need to
update our Composition files to match the new CRDs. In this case we extend managed resource migration with the additional steps.

12) Update `crossplane.yaml` file with official provider dependency.
13) Update Workspace resources within Composition files to the new API version `tf.upbound.io/v1beta1`, e.g.
```diff
   resources:
     - name: awsIAMRole
       base:
-        apiVersion: tf.crossplane.io/v1alpha1
+        apiVersion: tf.upbound.io/v1beta1
         kind: Workspace
         spec:
           forProvider:
```
14) Build and push the new configuration version

15) Update the configuration to the new version
```bash
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Configuration
metadata:
  name: ${configuration_name}
spec:
  package: ${configuration_registry}/${configuration_repository}:${new_version}
EOF
```

16) Update the Workspace resource reference within associated XR, e.g.

```bash
kubectl edit terraformxr.example.upbound.io/iam-role-demo-001-5zlnw
```

```diff
  resourceRefs:
  - apiVersion: tf.crossplane.io/v1alpha1
    kind: Workspace
-    name: iam-role-demo-001-5zlnw-n7n2q
+    name: iam-role-demo-001-5zlnw-khtnv
```

17) Remove the pause annnotation from associated Claim or XR (see step 6 where we
initially set it)

```bash
kubectl annotate terraformclaim.example.upbound.io/iam-role-demo-001 crossplane.io/paused=false
```

That's it! Your Configuration is successfully migrated to official Upbound
provider-terraform.
