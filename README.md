# provider-terraform

An **experimental** Crossplane provider for Terraform. Use this provider to
define new Crossplane Composite Resources (XRs) that are composed of a mix of
'native' Crossplane managed resources and your existing Terraform modules.

The Terraform provider adds support for a `Workspace` managed resource that
represents a Terraform workspace. The configuration of each workspace may be
either fetched from a remote source (e.g. git), or simply specified inline.

```yaml
apiVersion: tf.crossplane.io/v1alpha1
kind: Workspace
metadata:
  name: example-inline
  annotations:
    # The terraform workspace will be named 'coolbucket'. If you omitted this
    # annotation it would be derived from metadata.name - i.e. 'example-inline'.
    crossplane.io/external-name: coolbucket
spec:
  forProvider:
    # For simple cases you can use an inline source to specify the content of
    # main.tf as opaque, inline HCL.
    source: Inline
    module: |
      // All outputs are written to the connection secret.  Non-sensitive outputs
      // are stored as string values in the status.atProvider.outputs object.
      output "url" {
        value       = google_storage_bucket.example.self_link
      }

      resource "random_id" "example" {
        byte_length = 4
      }

      // The google provider and remote state are configured by the provider
      // config - see examples/providerconfig.yaml.
      resource "google_storage_bucket" "example" {
        name = "crossplane-example-${terraform.workspace}-${random_id.example.hex}"
      }
  writeConnectionSecretToRef:
    namespace: default
    name: terraform-workspace-example-inline
```

```yaml
apiVersion: tf.crossplane.io/v1alpha1
kind: Workspace
metadata:
  name: example-remote
  annotations:
    crossplane.io/external-name: myworkspace
spec:
  forProvider:
    # Use any module source supported by terraform init -from-module.
    source: Remote
    module: https://github.com/crossplane/tf
    # Variables can be specified inline, or loaded from a ConfigMap or Secret.
    vars:
      - key: region
        value: us-west-1
    varFiles:
      - source: SecretKey
        secretKeyRef:
          namespace: default
          name: terraform
          key: example.tfvar.json
  # All Terraform outputs are written to the connection secret.
  writeConnectionSecretToRef:
    namespace: default
    name: terraform-workspace-example-inline
```

## Installation

We highly encourage to use a declarative way of provider installation:

```sh
kubectl apply -f examples/install.yaml
```

Notice that in this example Provider resource is referencing ControllerConfig with debug enabled.

You can also setup the Terraform Provider using AWS
[IAM Roles for Service Accounts (IRSA)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).
For more information, check out the example [setup](./examples/aws-eks-irsa-seup.yaml), the process is
similar to what you would use for the
[provider-aws](https://github.com/crossplane-contrib/provider-aws/blob/master/AUTHENTICATION.md#using-iam-roles-for-serviceaccounts).

## Private Git repository support

To securely propagate git credentials create a `git-credentials` secret in [git credentials store] format.

```sh
cat .git-credentials
https://<user>:<token>@github.com

kubectl create secret generic git-credentials --from-file=.git-credentials
```

Reference it in ProviderConfig.

```yaml
apiVersion: tf.crossplane.io/v1alpha1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    - filename: .git-credentials # use exactly this filename
      source: Secret
      secretRef:
        namespace: crossplane-system
        name: git-credentials
        key: .git-credentials
```

Standard `.git-credentials` filename is important to keep so provider-terraform
controller will be able to automatically pick it up.

## Terraform Output support

Non-sensitive outputs are mapped to the status.atProvider.outputs section
as strings so they can be referenced by the Composition.
Strings, numbers and booleans can be referenced directly in Compositions
and can be used in the _convert_ transform if type conversion is needed.
Tuple and object outputs will be available in the corresponding JSON form.
This is required because undefined object attributes are not specified in the Workspace
CRD and so will be sanitized before the status is stored in the database.

That means that any output values required for use in the Composition must be published
explicitly and individually, and they cannot be referenced inside a tuple or object.

For example, the following terraform outputs:

```yaml
      output "string" {
        value = "bar"
        sensitive = false
      }
      output "number" {
        value = 1.9
        sensitive = false
      }
      output "object" {
        // This will be a JSON string - the key/value pairs are not accessible
        value = {"a": 3, "b": 2}
        sensitive = false
      }
      output "tuple" {
        // This will be a JSON string - the elements will not be accessible
        value = ["foo", "bar"]
        sensitive = false
      }
      output "bool" {
        value = false
        sensitive = false
      }
      output "sensitive" {
        value = "SENSITIVE"
        sensitive = true
      }
```

Appear in the corresponding outputs section as:

```yaml
status:
  atProvider:
    outputs:
      bool: "false"
      number: "1.9"
      object: '{"a":3,"b":2}'
      string: bar
      tuple: '["foo", "bar"]'
```

Note that the "sensitive" output is not included in status.atProvider.outputs

## Terraform CLI Command Arguments

Additional arguments can be passed to the Terraform plan, apply, and destroy commands by specifying
the planArgs, applyArgs and destroyArgs options.

For example:

```yaml
apiVersion: tf.crossplane.io/v1alpha1
kind: Workspace
metadata:
  name: example-args
spec:
  forProvider:
    # Run the terraform init command with -upgrade=true to upgrade any stored providers
    initArgs:
      - -upgrade=true
    # Run the terraform plan command with the -parallelism=2 argument
    planArgs:
      - -parallelism=2
    # Run the terraform apply command with the -target=specificresource argument
    applyArgs:
      - -target=specificresource
    # Run the terraform destroy command with the -refresh=false argument
    destroyArgs:
      - -refresh=false
    # Use any module source supported by terraform init -from-module.
    source: Remote
    module: https://github.com/crossplane/tf
  # All Terraform outputs are written to the connection secret.
  writeConnectionSecretToRef:
    namespace: default
    name: terraform-workspace-example-inline
```

This will cause the _terraform init_ command to be run with the "-upgrade=true" argument,
the _terraform plan_ command to be run with the -parallelism=2 argument,
the _terraform apply_ command to be run with the -target=specificresource argument,
and the _terraform destroy_ command to be run with the -refresh=false argument.

Note that by default the terraform _init_ command is run with the "-input=false", and "-no-color" arguments,
the terraform _apply_ and _destroy_ commands are run with the
"-no-color", "-auto-approve", and "-input=false" arguments, and the terraform _plan_ command is
run with the "-no-color", "-input=false", and "-detailed-exitcode" arguments. Arguments specified in
applyArgs, destroyArgs and planArgs will be added to these default arguments.

## Known limitations

- You must either use remote state or ensure the provider container's `/tf`
  directory is not lost. `provider-terraform` **does not persist state**;
  consider using the [Kubernetes] remote state backend.
- If the module takes longer than the supplied `--timeout` to apply the
  underlying `terraform` process will be killed. You will potentially lose state
  and leak resources.
- The provider won't emit an event until _after_ it has successfully applied the
  Terraform module, which can take a long time.

[kubernetes]: https://www.terraform.io/docs/language/settings/backends/kubernetes.html
[git credentials store]: https://git-scm.com/docs/git-credential-store
