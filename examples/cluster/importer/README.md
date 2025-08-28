# Workaround for Importing Resources Functionality

This example Configuration(Composition+XRD) demonstrates a temporary workaround
for Importing Resources functionality before it is fully automated in the core Crossplane.

In a nutshell it is a well-known
https://docs.crossplane.io/knowledge-base/guides/import-existing-resources/#import-resources-manually
process that is automated via the Composition.

The workaround consists of a `Composition` that provides a mix of provider-terraform
`Workspace` with the
[aws_vpc](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/vpc)
**data** resource as Inline module.

It publishes the discovered imported  `vpcdata` to the `XSubnet` XR status.

The `vpcdata.id` and other data from the status is getting eventually consumed by `VPC`
provider-aws resource following which is a part of the same `Composition`. `VPC`
is eventually used as a reference in a new standard `Subnet` creation.
