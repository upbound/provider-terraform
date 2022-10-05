FROM hashicorp/terraform:1.2.3

ARG TARGETOS
ARG TARGETARCH

ADD bin/$TARGETOS\_$TARGETARCH/provider /usr/local/bin/crossplane-terraform-provider
ADD .gitconfig .gitconfig

# As of Crossplane v1.3.0 provider controllers run as UID 2000.
# https://github.com/crossplane/crossplane/blob/v1.3.0/internal/controller/pkg/revision/deployment.go#L32
RUN mkdir /tf
RUN chown 2000 /tf

USER 65532
ENTRYPOINT ["crossplane-terraform-provider"]