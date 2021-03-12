FROM BASEIMAGE

ARG ARCH
ARG TINI_VERSION

ADD provider /usr/local/bin/crossplane-terraform-provider

RUN mkdir /tf
RUN chown 1001 /tf

EXPOSE 8080
USER 1001
ENTRYPOINT ["crossplane-terraform-provider"]
