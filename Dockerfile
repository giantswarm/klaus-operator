FROM gsoci.azurecr.io/giantswarm/alpine:3.23.3

RUN apk add --no-cache ca-certificates

ADD ./klaus-operator /klaus-operator

USER 65532:65532

ENTRYPOINT ["/klaus-operator"]
