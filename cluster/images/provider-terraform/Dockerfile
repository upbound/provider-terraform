FROM alpine:3.20.3
RUN apk --no-cache add ca-certificates bash git curl
ARG TARGETOS
ARG TARGETARCH

ENV TERRAFORM_VERSION=1.5.5
ENV TF_IN_AUTOMATION=1
ENV TF_DIR=/tf/

ADD "bin/${TARGETOS}_${TARGETARCH}/provider" /usr/local/bin/crossplane-terraform-provider
ADD .gitconfig .gitconfig

# Do not change the URL from which the Terraform CLI is downloaded.
# We are using an MPL-2.0 licensed version of the CLI and
# we must make sure that this holds true.
RUN curl -s -L https://github.com/upbound/terraform/releases/download/v${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_${TARGETOS}_${TARGETARCH}.zip -o terraform.zip \
  && unzip -d /usr/local/bin terraform.zip \
  && rm terraform.zip \
  && chmod +x /usr/local/bin/terraform \
  && mkdir -p ${TF_DIR} \
  && chown -R 2000 /tf
# As of Crossplane v1.3.0 provider controllers run as UID 2000.
# https://github.com/crossplane/crossplane/blob/v1.3.0/internal/controller/pkg/revision/deployment.go#L32

USER 65532
ENTRYPOINT ["crossplane-terraform-provider"]
