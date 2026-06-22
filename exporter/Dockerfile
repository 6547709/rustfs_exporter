# syntax=docker/dockerfile:1.6
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/exporter ./cmd/exporter

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/exporter /exporter
EXPOSE 9090
USER nonroot
ENTRYPOINT ["/exporter"]