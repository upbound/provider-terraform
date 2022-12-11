---
title: Configuration
weight: 2
---
# Configuration Options

There are several ways to provide configurations to the official Terraform
provider that will propagate to the underlying Terraform workspace. In the
following sections, we will cover the most common ones.

## IAM Roles for Service Accounts (IRSA)

You can setup the Terraform Provider using AWS [IAM Roles for Service Accounts
(IRSA)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).
For more information, check out the example
[setup](../examples/aws-eks-irsa-setup.yaml), the process is similar to what you
would use for the
[provider-aws](https://github.com/upbound/provider-aws/blob/master/AUTHENTICATION.md#using-iam-roles-for-serviceaccounts).

## Private Git repository support

To securely propagate git credentials create a `git-credentials` secret in [git
credentials store] format.

```sh
cat .git-credentials
https://<user>:<token>@github.com

kubectl create secret generic git-credentials --from-file=.git-credentials
```

Reference it in ProviderConfig.

```yaml
apiVersion: tf.upbound.io/v1beta1
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
...
```

Standard `.git-credentials` filename is important to keep so provider-terraform
controller will be able to automatically pick it up.

## Terraform Output support

Non-sensitive outputs are mapped to the status.atProvider.outputs section as
strings so they can be referenced by the Composition. Strings, numbers and
booleans can be referenced directly in Compositions and can be used in the
_convert_ transform if type conversion is needed. Tuple and object outputs will
be available in the corresponding JSON form. This is required because undefined
object attributes are not specified in the Workspace CRD and so will be
sanitized before the status is stored in the database.

That means that any output values required for use in the Composition must be
published explicitly and individually, and they cannot be referenced inside a
tuple or object.

For example, the following terraform outputs:
```hcl
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
Additional arguments can be passed to the Terraform plan, apply, and destroy
commands by specifying the planArgs, applyArgs and destroyArgs options.

For example:
```yaml
apiVersion: tf.upbound.io/v1beta1
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
This will cause the _terraform init_ command to be run with the "-upgrade=true"
argument, the _terraform plan_ command to be run with the -parallelism=2
argument, the _terraform apply_ command to be run with the
-target=specificresource argument, and the _terraform destroy_ command to be run
with the -refresh=false argument.

Note that by default the terraform _init_ command is run with the
"-input=false", and "-no-color" arguments, the terraform _apply_ and _destroy_
commands are run with the "-no-color", "-auto-approve", and "-input=false"
arguments, and the terraform _plan_ command is run with the "-no-color",
"-input=false", and "-detailed-exitcode" arguments.  Arguments specified in
applyArgs, destroyArgs and planArgs will be added to these default arguments.

## Custom Entrypoint for Terraform Invocation

In some cases, you might want to initialize and apply terraform in the
subdirectory of the repository checkout. It is most relevant for the cases when
your terraform modules contain inline [relative paths](#83).

To enable it, the `Workspace` spec has an **optional** `Entrypoint` field.

Consider this example:

```yml
apiVersion: tf.upbound.io/v1beta1
kind: Workspace
metadata:
  name: relative-path-test
spec:
  forProvider:
    module: git::https://github.com/ytsarev/provider-terraform-test-module.git
    source: Remote
    entrypoint: relative-path-iam
    vars:
      - key: iamRole
        value: relative-path-test
```

In this case, the whole repository will be checked out but terraform will be
initialized in the `relative-path-iam` subdirectory with the module that
contains relative path reference to the `iam` module located in the root of the
tree.

```HCL
module "relative-path-iam" {
  source  = "../iam"
  iamRole = var.iamRole
}
```

## Provider Plugin Cache(enabled by default)

[Provider Plugin
Cache](https://developer.hashicorp.com/terraform/cli/config/config-file#provider-plugin-cache)
is enabled by default to speed up reconciliation.

In case you need to disable it, set optional `pluginCache` to `false` in
`ProviderConfig`:

```console
apiVersion: tf.upbound.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  pluginCache: false
...
```

## Enable External Secret Support

If you need to store the sensitive output to an external secret store like
Vault, you can specify the `--enable-external-secret-stores` flag to enable it:

```yaml
apiVersion: pkg.crossplane.io/v1alpha1
kind: ControllerConfig
metadata:
  name: terraform-config
  labels:
    app: crossplane-provider-terraform
spec:
  image: crossplane/provider-terraform-controller:v0.3.0
  args:
    - -d
    - --enable-external-secret-stores
  metadata:
    annotations:
      vault.hashicorp.com/agent-inject: "true"
      vault.hashicorp.com/agent-inject-token: "true"
      vault.hashicorp.com/role: "crossplane"
      vault.hashicorp.com/agent-run-as-user: "2000"
```

Prepare a `StoreConfig` for Vault:
```yaml
apiVersion: tf.upbound.io/v1beta1
kind: StoreConfig
metadata:
  name: vault
spec:
  type: Vault
  defaultScope: crossplane-system
  vault:
    server: http://vault.vault-system:8200
    mountPath: secret/
    version: v2
    auth:
      method: Token
      token:
        source: Filesystem
        fs:
          path: /vault/secrets/token
```

Specify it in `spec.publishConnectionDetailsTo`:
```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: ...
  labels:
    feature: ess
spec:
  compositeTypeRef:
    apiVersion: ...
    kind: ...
  resources:
    - name: foo
      base:
        apiVersion: tf.upbound.io/v1beta1
        kind: Workspace
        metadata:
          name: foo
        spec:
          forProvider:
            ...
          publishConnectionDetailsTo:
            name: bar
            configRef:
              name: vault
```

At Vault side configuration is also needed to allow the write operation, see
[example](https://crossplane.io/docs/v1.9/guides/vault-as-secret-store.html#prepare-vault)
here for inspiration.
