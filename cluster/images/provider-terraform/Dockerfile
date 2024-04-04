FROM alpine:3.19.1
RUN apk --no-cache add ca-certificates bash git curl
ARG TARGETOS
ARG TARGETARCH

ENV TENV_VERSION=1.5.0
ENV TENV_ROOT=/.tenv
ENV TFENV_TERRAFORM_DEFAULT_VERSION=1.5.5
ENV TF_IN_AUTOMATION=1
ENV TF_PLUGIN_CACHE_DIR=/tf/plugin-cache

ADD "bin/${TARGETOS}_${TARGETARCH}/provider" /usr/local/bin/crossplane-terraform-provider
ADD .gitconfig .gitconfig

# Do not change the URL from which the Terraform CLI is downloaded.
# We are using an MPL-2.0 licensed version of the CLI and
# we must make sure that this holds true.
RUN wget https://github.com/tofuutils/tenv/releases/download/v${TENV_VERSION}/tenv_v${TENV_VERSION}_amd64.apk \
  && apk add --allow-untrusted tenv_v${TENV_VERSION}_amd64.apk \
  && rm tenv_v${TENV_VERSION}_amd64.apk \
  && mkdir -p ${TF_PLUGIN_CACHE_DIR} \
  && tenv tf install ${TFENV_TERRAFORM_DEFAULT_VERSION} \
  && chown -R 2000 /tf /.tenv/
# As of Crossplane v1.3.0 provider controllers run as UID 2000.
# https://github.com/crossplane/crossplane/blob/v1.3.0/internal/controller/pkg/revision/deployment.go#L32

USER 65532
ENTRYPOINT ["crossplane-terraform-provider"]
