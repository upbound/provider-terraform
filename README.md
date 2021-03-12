# provider-terraform

An __experimental__ Crossplane provider for Terraform. Use this provider to
define new Crossplane XRs that are composed of a mix of 'native' Crossplane
managed resources and your existing Terraform modules.

```yaml
apiVersion: tf.crossplane.io/v1alpha1
kind: Workspace
metadata:
  name: example-remote
  annotations:
    # The terraform workspace will be named 'myworkspace'. If you omit this
    # annotation it would be derived from metadata.name - e.g. 'example-remote'.
    crossplane.io/external-name: myworkspace
spec:
  forProvider:
    # Use any module source supported by terraform init -from-module. You can
    # also specify a simple main.tf inline; see examples/example-inline.
    module: https://github.com/crossplane/tf
    # Variables can be specified inline.
    vars:
    - key: region
      value: us-west-1
    # Variable files can be loaded from a ConfigMap or a Secret.
    varFiles:
    - source: ConfigMapKey
      configMapKeyRef:
        namespace: default
        name: terraform
        key: example.tfvars
    - source: SecretKey
      secretKeyRef:
        namespace: default
        name: terraform
        key: example.tfvar.json
      # Variables are expected to be in HCL '.tfvars' format by default. Use
      # the JSON format if your variables are in the JSON '.tfvars.json' format.
      format: JSON
  # All Terraform outputs are written to the connection secret.
  writeConnectionSecretToRef:
    namespace: default
    name: terraform-workspace-example-inline
```

Known limitations:

* You must either use remote state or ensure the provider container's `/tf`
  directory is not lost. `provider-terraform` __does not persist state__;
  consider using the [Kubernetes] remote state backend.
* If the module takes longer than 20 minutes to apply the underlying `terraform`
  process will be killed. You will potentially lose state and leak resources.
* The provider won't emit an event until _after_ it has successfully applied the
  Terraform module, which can take a long time.
* Each `Workspace` is allocated a directory under the `/tf` directory inside the
  provider container. These directories are not yet garbage collected when a
  Workspace is deleted.

[Kubernetes]: https://www.terraform.io/docs/language/settings/backends/kubernetes.html