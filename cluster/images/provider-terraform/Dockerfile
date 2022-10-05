FROM hashicorp/terraform:1.2.3

ARG TARGETOS
ARG TARGETARCH

ADD bin/$TARGETOS\_$TARGETARCH/provider /usr/local/bin/crossplane-terraform-provider
ADD .gitconfig .gitconfig

USER 65532
ENTRYPOINT ["crossplane-terraform-provider"]