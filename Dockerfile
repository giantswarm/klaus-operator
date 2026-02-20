FROM --platform=$BUILDPLATFORM golang:1.26.0 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-w -extldflags '-static'" \
    -o klaus-operator .

FROM gsoci.azurecr.io/giantswarm/alpine:3.23.3

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/klaus-operator /klaus-operator

USER 65532:65532

ENTRYPOINT ["/klaus-operator"]
