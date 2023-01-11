# provider-terraform demo sample

This example demonstrates `XSubnet` consistent XRD abstraction with multiple
`Composition` implementations.

It illustrates the following transition:

1. Barebone Managed Resource of [Terraform Workspace](00-mr-tf-workspace/workspace-inline.yaml).
2. [Terraform Workspace within a Composition](01-composition-tf-only/composition.yaml).
3. Mixed scenario of [Terraform Workspace and Crossplane-native Resource within the Composition](02-composition-tf-and-native/composition.yaml).
4. Full migration to [Crossplane-native Managed Resources](03-composition-native-only/composition.yaml).

All Composition-based steps are backed by the stable custom API defined by [XRD](definition.yaml).

All Composition-based transition stages can be end-to-end tested by associated
Claim instantiation.
