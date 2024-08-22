---
title: Quickstart
weight: 1
---
# Quickstart

The official Terraform Provider allows you to write Terraform HCL configuration
as the desired configuration for your infrastructure. It allows you to use your
existing Terraform modules from different sources and still get the experience
of using Crossplane with all the features like automatic drift correction,
composition and others.

This guide walks through the process to install Upbound Universal Crossplane and
install the Terraform official provider.

To use this official provider, install Upbound Universal Crossplane into your
Kubernetes cluster, install the `Provider`, apply a `ProviderConfig`, and create
a *managed resource* of type `Workspace` via Kubernetes.

## Install the Up command-line
Download and install the Upbound `up` command-line.

```shell
curl -sL "https://cli.upbound.io" | sh
mv up /usr/local/bin/
```

Verify the version of `up` with `up --version`

```shell
$ up version
v0.13.0
```

## Install Universal Crossplane
Install Upbound Universal Crossplane with the Up command-line.

```shell
$ up uxp install
UXP 1.10.1-up.1 installed
```

Verify the UXP pods are running with `kubectl get pods -n upbound-system`

```shell
$ kubectl get pods -n upbound-system
NAME                                       READY   STATUS    RESTARTS      AGE
crossplane-ddc974f67-kp6t2                 1/1     Running   0             93s
crossplane-rbac-manager-7978c5f8df-8w8sg   1/1     Running   0             93s
upbound-bootstrapper-754f65bd-h92tm        1/1     Running   0             93s
xgql-8fb949dcf-pxn4z                       1/1     Running   3 (52s ago)   93s
```

## Install the official Terraform provider

Install the official provider into the Kubernetes cluster with a Kubernetes
configuration file. 

```yaml
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-terraform
spec:
  package: xpkg.upbound.io/upbound/provider-terraform:<version>
```

Apply this configuration with `kubectl apply -f`.

After installing the provider, verify the install with `kubectl get providers`.   

```shell
$ kubectl get providers
NAME                 INSTALLED   HEALTHY   PACKAGE                                             AGE
provider-terraform   True        True      xpkg.upbound.io/upbound/provider-terraform:v0.1.0   15s
```

It may take up to 5 minutes to report `HEALTHY`.

## Create a Kubernetes secret
The provider requires credentials to create and manage cloud resources. In this
guide, we will work with GCP as an example, but the process is the same for any
cloud provider.

### Generate a GCP JSON key file
Create a JSON key file containing the GCP account credentials. GCP provides
documentation on [how to create a key
file](https://cloud.google.com/iam/docs/creating-managing-service-account-keys).

Here is an example key file:

```json
{
  "type": "service_account",
  "project_id": "caramel-goat-354919",
  "private_key_id": "e97e40a4a27661f12345678f4bd92139324dbf46",
  "private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvwIBADANBgkqhkiG9w0BAQEFAASCBKkwggSlAgEAAoIBAQCwA+6MWRhmcPB3\nF/irb5MDPYAT6BWr7Vu/16U8FbCHk7xtsAWYjKXKHu5mGzum4F781sM0aMCeitlv\n+jr2y7Ny23S9uP5W2kfnD/lfj0EjCdfoaN3m7W0j4DrriJviV6ESeSdb0Ehg+iEW\ngNrkb/ljigYgsSLMuemby5lvJVINUazXJtGUEZew+iAOnI4/j/IrDXPCYVNo5z+b\neiMsDYWfccenWGOQf1hkbVWyKqzsInxu8NQef3tNhoUXNOn+/kgarOA5VTYvFUPr\n2l1P9TxzrcYuL8XK++HjVj5mcNaWXNN+jnFpxjMIJOiDJOZoAo0X7tuCJFXtAZbH\n9P61GjhbAgMBAAECggEARXo31kRw4jbgZFIdASa4hAXpoXHx4/x8Q9yOR4pUNR/2\nt+FMRCv4YTEWb01+nV9hfzISuYRDzBEIxS+jyLkda0/+48i69HOTAD0I9VRppLgE\ne97e40a4a27661f12345678f4bd92139324dbf46+2H7ulQDtbEgfcWpNMQcL2JiFq+WS\neh3H0gHSFFIWGnAM/xofrlhGsN64palZmbt2YiKXcHPT+WgLbD45mT5j9oMYxBJf\nPkUUX5QibSSBQyvNqCgRKHSnsY9yAkoNTbPnEV0clQ4FmSccogyS9uPEocQDefuY\nY7gpwSzjXpaw7tP5scK3NtWmmssi+dwDadfLrKF7oQKBgQDjIZ+jwAggCp7AYB/S\n6dznl5/G28Mw6CIM6kPgFnJ8P/C/Yi2y/OPKFKhMs2ecQI8lJfcvvpU/z+kZizcG\nr/7iRMR/SX8n1eqS8XfWKeBzIdwQmiKyRg2AKelGKljuVtI8sXKv9t6cm8RkWKuZ\n9uVroTCPWGpIrh2EMxLeOrlm0QKBgQDGYxoBvl5GfrOzjhYOa5GBgGYYPdE7kNny\nhpHE9CrPZFIcb5nGMlBCOfV+bqA9ALCXKFCr0eHhTjk9HjHfloxuxDmz34vC0xXG\ncegqfV9GNKZPDctysAlCWW/dMYw4+tzAgoG9Qm13Iyfi2Ikll7vfeMX7fH1cnJs0\nnYpN9LYPawKBgQCwMi09QoMLGDH+2pLVc0ZDAoSYJ3NMRUfk7Paqp784VAHW9bqt\n1zB+W3gTyDjgJdTl5IXVK+tsDUWu4yhUr8LylJY6iDF0HaZTR67HHMVZizLETk4M\nLfvbKKgmHkPO4NtG6gEmMESRCOVZUtAMKFPhIrIhAV2x9CBBpb1FWBjrgQKBgQCj\nkP3WRjDQipJ7DkEdLo9PaJ/EiOND60/m6BCzhGTvjVUt4M22XbFSiRrhXTB8W189\noZ2xrGBCNQ54V7bjE+tBQEQbC8rdnNAtR6kVrzyoU6xzLXp6Wq2nqLnUc4+bQypT\nBscVVfmO6stt+v5Iomvh+l+x05hAjVZh8Sog0AxzdQKBgQCMgMTXt0ZBs0ScrG9v\np5CGa18KC+S3oUOjK/qyACmCqhtd+hKHIxHx3/FQPBWb4rDJRsZHH7C6URR1pHzJ\nmhCWgKGsvYrXkNxtiyPXwnU7PNP9JNuCWa45dr/vE/uxcbccK4JnWJ8+Kk/9LEX0\nmjtDm7wtLVlTswYhP6AP69RoMQ==\n-----END PRIVATE KEY-----\n",
  "client_email": "my-sa-313@caramel-goat-354919.iam.gserviceaccount.com",
  "client_id": "103735491955093092925",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://oauth2.googleapis.com/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/my-sa-313%40caramel-goat-354919.iam.gserviceaccount.com"
}
```

Save this JSON file as `gcp-credentials.json`.

### Create a Kubernetes secret with GCP credentials
Use the following command to create the Kubernetes secret object in the cluster.

```
kubectl create secret generic tf-gcp-creds -n upbound-system \
  --from-file=credentials=./gcp-credentials.json
```

View the secret with `kubectl describe secret tf-gcp-creds -n upbound-system`

```shell
$ kubectl describe secret gcp-creds -n upbound-system
Name:         tf-gcp-creds
Namespace:    upbound-system
Labels:       <none>
Annotations:  <none>

Type:  Opaque

Data
====
credentials:  2380 bytes
```

## Create a ProviderConfig

Create a `ProviderConfig` Kubernetes configuration file to attach the GCP
credentials to the installed official provider.

**Note:** the `ProviderConfig` must contain the correct GCP project ID. The
project ID must match the `project_id` from the JSON key file.

```yaml
apiVersion: tf.upbound.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  # Note that unlike most provider configs this one supports an array of
  # credentials. This is because each Terraform workspace uses a single
  # Crossplane provider config, but could use multiple Terraform providers each
  # with their own credentials.
  credentials:
    - filename: gcp-credentials.json
      source: Secret
      secretRef:
        namespace: upbound-system
        name: tf-gcp-creds
        key: credentials
  # This optional configuration block can be used to inject HCL into any
  # workspace that uses this provider config, for example to setup Terraform
  # providers.
  configuration: |
    provider "google" {
      credentials = "gcp-credentials.json"
      project     = "YOUR-GCP-PROJECT-ID"
    }

    // Modules _must_ use remote state. The provider does not persist state.
    terraform {
      backend "kubernetes" {
        secret_suffix     = "providerconfig-default"
        namespace         = "upbound-system"
        in_cluster_config = true
      }
    }
```

Apply this configuration with `kubectl apply -f`.

**Note:** the `ProviderConfig` value `spec.credentials[0].secretRef.name` must
match the `name` of the secret in `kubectl get secrets -n upbound-system` and
`spec.secretRef.key` must match the value in the `Data` section of the secret.

Verify the `ProviderConfig` with `kubectl describe providerconfigs`. 

```yaml
$ kubectl describe providerconfigs
Name:         default
Namespace:
API Version:  tf.upbound.io/v1beta1
Kind:         ProviderConfig
# Output truncated
Spec:
  Configuration:  provider "google" {
  credentials = "gcp-credentials.json"
  project     = "official-provider-testing"
  }

  // Modules _must_ use remote state. The provider does not persist state.
  terraform {
      backend "kubernetes" {
      secret_suffix     = "providerconfig-default"
      namespace         = "upbound-system"
      in_cluster_config = true
    }
  }
  Credentials:
    Filename:  gcp-credentials.json
    Secret Ref:
      Key:        credentials
      Name:       gcp-creds
      Namespace:  upbound-system
    Source:       Secret
  Plugin Cache:   true
```

## Create a `Workspace` resource
Create a managed resource of type `Workspace` to verify the provider is
functioning. 

This example creates a GCP storage bucket with a globally unique name.

```yaml
apiVersion: tf.upbound.io/v1beta1
kind: Workspace
metadata:
  name: example-inline
  annotations:
    # The terraform workspace will be named 'coolbucket'. If you omit this
    # annotation it would be derived from metadata.name - e.g. 'example-inline'.
    crossplane.io/external-name: coolbucket
spec:
  forProvider:
    # Workspaces default to using a remote source - like workspace-remote.yaml.
    # For simple cases you can use an inline source to specify the content of
    # main.tf as opaque, inline HCL.
    source: Inline
    module: |
      // Outputs are written to the connection secret.
      output "url" {
        value       = google_storage_bucket.example.self_link
      }

      resource "random_id" "example" {
        byte_length = 4
      }

      // The google provider and remote state are configured by the provider
      // config - see providerconfig.yaml.
      resource "google_storage_bucket" "example" {
        name = "crossplane-example-${terraform.workspace}-${random_id.example.hex}"
        location      = "US"
        force_destroy = true

        public_access_prevention = "enforced"
      }
  writeConnectionSecretToRef:
    namespace: default
    name: terraform-workspace-example-inline
```

**Note:** the `spec.providerConfigRef.name` must match the `ProviderConfig`
`metadata.name` value.

Apply this configuration with `kubectl apply -f`.

Use `kubectl get workspace` to verify Terraform code successfully executed.

```shell
$ kubectl get workspace
NAME             READY   SYNCED   AGE
example-inline   True    True     46s
```

Provider applied the core in the workspace when the values `READY` and `SYNCED`
are `True`. Since the workspace resource we configured creates a
`google_storage_bucket` resource, we can verify that the bucket was created by
checking the GCP console.

If the `READY` or `SYNCED` are blank or `False` use `kubectl describe` to
understand why.

## Delete the managed resource
Remove the workspace by using `kubectl delete -f` with the same `Workspace`
object file. The provider triggers a `terraform destroy` and removes the bucket
with the deletion of the `Workspace` resource.

Verify the removal of the bucket with `kubectl get workspace`.

```shell
$ kubectl get workspace
No resources found
```
