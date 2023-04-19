FROM --platform=$TARGETPLATFORM golang:1.17-alpine AS builder
WORKDIR /app
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download
COPY main.go main.go
COPY controllers/ controllers/
ARG TARGETPLATFORM TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o exposer-controller .

FROM alpine AS runtime
WORKDIR /
COPY --from=builder /app/exposer-controller .

ENTRYPOINT ["/exposer-controller"]
