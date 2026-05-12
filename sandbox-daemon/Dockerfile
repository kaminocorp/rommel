# Multi-stage build. Build context is the repo root so the daemon can see
# its sibling proto/clients/go module via the replace directive in go.mod.
#
#   docker build -f sandbox-daemon/Dockerfile -t rommel-sandbox-daemon .

FROM golang:1.22 AS build
WORKDIR /src

# Bring in the proto Go client first (replace target) so go mod can resolve it.
COPY proto/clients/go ./proto/clients/go
# Regenerate the Go client from schemas. Gitignored on disk; required to compile.
COPY proto ./proto
RUN bash ./proto/codegen/go.sh

COPY sandbox-daemon ./sandbox-daemon
WORKDIR /src/sandbox-daemon
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sandbox-daemon ./cmd/daemon

FROM debian:stable-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tini \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/sandbox-daemon /usr/local/bin/sandbox-daemon
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/sandbox-daemon"]
