# syntax=docker/dockerfile:1.7.0@sha256:dbbd5e059e8a07ff7ea6233b213b36aa516b4c53c645f1817a4dd18b83cbea56

# Multi-architecture manifest digests audited on 2026-07-19.
ARG GO_IMAGE=golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651
ARG DEBIAN_IMAGE=debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN set -eux; \
    install -d /out; \
    version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"; \
    contract_version="$(tr -d ' \n\r' < internal/version/contract.version)"; \
    test -n "$version"; \
    test -n "$contract_version"; \
    case "$TARGETARCH" in \
      amd64) arch_env='GOAMD64=v1' ;; \
      arm64) arch_env='GOARM64=v8.0' ;; \
      *) echo "unsupported target architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    env GOWORK=off GOFLAGS='' GOEXPERIMENT='' GOFIPS140=off \
      CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" $arch_env \
      go build -mod=readonly -buildvcs=false -trimpath \
      -ldflags="-s -w -X github.com/luxiaba/remnanode-lite/internal/version.Version=$version -X github.com/luxiaba/remnanode-lite/internal/version.ContractVersion=$contract_version" \
      -o /out/remnanode-lite ./cmd/remnanode-lite; \
    env GOWORK=off GOFLAGS='' GOEXPERIMENT='' GOFIPS140=off CGO_ENABLED=0 \
      go build -mod=readonly -buildvcs=false -trimpath \
      -ldflags='-s -w' -o /out/asn-builder ./cmd/asn-builder; \
    env GOWORK=off GOFLAGS='' GOEXPERIMENT='' GOFIPS140=off CGO_ENABLED=0 \
      go build -mod=readonly -buildvcs=false -trimpath \
      -ldflags='-s -w' -o /out/release-tool ./cmd/release-tool

FROM --platform=$BUILDPLATFORM ${DEBIAN_IMAGE} AS assets

ARG TARGETARCH

COPY --from=build /out/asn-builder /usr/local/bin/asn-builder
COPY --from=build /out/release-tool /usr/local/bin/release-tool
COPY release/runtime-assets.lock.json /runtime-assets.lock.json

RUN --mount=type=cache,target=/var/cache/remnanode-runtime-assets,sharing=locked \
    set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates; \
    rm -rf /var/lib/apt/lists/*; \
    release-tool materialize \
      --lock /runtime-assets.lock.json \
      --arch "$TARGETARCH" \
      --asn-builder /usr/local/bin/asn-builder \
      --cache-dir /var/cache/remnanode-runtime-assets \
      --out-dir /assets; \
    rm -f /usr/local/bin/asn-builder \
      /usr/local/bin/release-tool \
      /runtime-assets.lock.json

FROM ${DEBIAN_IMAGE} AS runtime

LABEL org.opencontainers.image.title="Remnanode Lite" \
      org.opencontainers.image.description="Low-memory Remnawave Node compatible implementation" \
      org.opencontainers.image.source="https://github.com/luxiaba/remnanode-lite" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates nftables; \
    rm -rf /var/lib/apt/lists/*; \
    install -d -m 0755 \
      /usr/local/lib/remnanode \
      /usr/local/share/remnanode/xray \
      /usr/local/share/remnanode/asn \
      /usr/share/doc/remnanode-lite/licenses \
      /run/remnanode \
      /var/log/remnanode

COPY --from=build --chmod=0755 /out/remnanode-lite /usr/local/bin/remnanode-lite
COPY --from=assets --chmod=0755 /assets/lib/rw-core /usr/local/lib/remnanode/rw-core
COPY --from=assets --chmod=0644 /assets/share/xray/geoip.dat /usr/local/share/remnanode/xray/geoip.dat
COPY --from=assets --chmod=0644 /assets/share/xray/geosite.dat /usr/local/share/remnanode/xray/geosite.dat
COPY --from=assets --chmod=0644 /assets/share/asn/asn-prefixes.bin /usr/local/share/remnanode/asn/asn-prefixes.bin
COPY --from=assets --chmod=0644 /assets/licenses/CC-BY-SA-4.0.txt /usr/share/doc/remnanode-lite/licenses/CC-BY-SA-4.0.txt
COPY --from=assets --chmod=0644 /assets/licenses/CC0-1.0.txt /usr/share/doc/remnanode-lite/licenses/CC0-1.0.txt
COPY --from=assets --chmod=0644 /assets/licenses/GPL-3.0-only.txt /usr/share/doc/remnanode-lite/licenses/GPL-3.0-only.txt
COPY --from=assets --chmod=0644 /assets/licenses/MPL-2.0.txt /usr/share/doc/remnanode-lite/licenses/MPL-2.0.txt

# Keep the runtime image self-describing. These small files let an operator
# inspect the exact license, provenance, and source-offer terms without the
# source checkout or a separate release archive.
COPY --chmod=0644 LICENSE /usr/share/doc/remnanode-lite/LICENSE
COPY --chmod=0644 release/bundle/THIRD_PARTY_NOTICES.md /usr/share/doc/remnanode-lite/THIRD_PARTY_NOTICES.md
COPY --chmod=0644 release/bundle/SOURCE-OFFER.md /usr/share/doc/remnanode-lite/SOURCE-OFFER.md
COPY --chmod=0644 release/runtime-assets.lock.json /usr/share/doc/remnanode-lite/runtime-assets.lock.json

ENV XRAY_BIN=/usr/local/lib/remnanode/rw-core \
    GEO_DIR=/usr/local/share/remnanode/xray \
    ASN_DB_PATH=/usr/local/share/remnanode/asn/asn-prefixes.bin \
    LOG_DIR=/var/log/remnanode \
    INTERNAL_SOCKET_PATH=/run/remnanode/internal.sock

EXPOSE 2222
STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/remnanode-lite"]
