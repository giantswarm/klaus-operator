FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /klaus-operator .

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /klaus-operator /klaus-operator

USER 65532:65532

ENTRYPOINT ["/klaus-operator"]
