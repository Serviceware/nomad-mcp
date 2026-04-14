FROM --platform=$BUILDPLATFORM golang:1.26.1-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/nomad-mcp ./cmd/nomad-mcp

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/nomad-mcp /nomad-mcp

ENTRYPOINT ["/nomad-mcp"]