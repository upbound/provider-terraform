FROM alpine:3.16.2
RUN apk --no-cache add ca-certificates bash git curl
ARG TARGETOS
ARG TARGETARCH

ENV TERRAFORM_VERSION=1.3.4

ADD "bin/${TARGETOS}_${TARGETARCH}/provider" /usr/local/bin/crossplane-terraform-provider
ADD .gitconfig .gitconfig

RUN curl -s -L https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_${TARGETOS}_${TARGETARCH}.zip | \
  unzip -d /usr/local/bin - \
  && chmod +x /usr/local/bin/terraform
# As of Crossplane v1.3.0 provider controllers run as UID 2000.
# https://github.com/crossplane/crossplane/blob/v1.3.0/internal/controller/pkg/revision/deployment.go#L32
RUN mkdir /tf
RUN chown 2000 /tf

USER 65532
ENTRYPOINT ["crossplane-terraform-provider"]
