FROM BASEIMAGE

ARG ARCH
ARG TINI_VERSION

ADD provider /usr/local/bin/crossplane-terraform-provider
ADD .gitconfig .gitconfig

# As of Crossplane v1.3.0 provider controllers run as UID 2000.
# https://github.com/crossplane/crossplane/blob/v1.3.0/internal/controller/pkg/revision/deployment.go#L32
RUN mkdir /tf
RUN chown 2000 /tf

EXPOSE 8080
USER 2000
ENTRYPOINT ["crossplane-terraform-provider"]
