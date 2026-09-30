# cardo as a container image: the same single static binary, for platforms that deploy images
# rather than copying a file onto a host.
#
# The reference stack builds this to run `cardo migrate` once before the collector starts, since
# the collector only ever INSERTs and its tables must already exist (sql/clickhouse/004).

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY sql ./sql
# No `go mod download`: go.mod's require block is empty (ADR-0022), so there is nothing to fetch.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cardo ./cmd/cardo

FROM scratch
# `cardo poll` talks to api.anthropic.com over TLS, and scratch carries no CA bundle of its own.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/cardo /cardo
USER 65534:65534
ENTRYPOINT ["/cardo"]
