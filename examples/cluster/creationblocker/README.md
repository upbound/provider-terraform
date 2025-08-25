# Blocking condition PoC

A very special edge case workaround, use it at your own risk :)

This example Configuration(Composition+XRD) demonstrates a blocking condition
PoC for blocking the resource creation according to the result of some special
condition.

We use provider-terraform Workspace to create the special condition and check if
VPC alredy exists and in case it does it fully blocks the further execution by
intentionally violating the RFC1123 in metada.name of composed VPC resource.
