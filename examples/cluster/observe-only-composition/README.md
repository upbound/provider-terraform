# Workaround for Observe-Only Resources Functionality

This example Configuration(Composition+XRD) demonstrates a temporary workaround
for Observe-Only Resources functionality before it is [properly
implemented](https://github.com/crossplane/crossplane/issues/1722)
the core Crossplane.

The workaround consists of a `Composition` that provides a mix of provider-terraform
`Workspace` with the
[aws_vpc](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/vpc)
**data** resource as Inline module.

It publishes the discovered observe-only `vpcId` to the `XSubnet` XR status.

The `vpcId` from the status is getting eventually consumed by the native `Subnet`
provider-aws resource which is a part of the same `Composition`.
