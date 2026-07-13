# Builds spageth: clones upstream go-ethereum, applies the spageth patch +
# overlay, wires the xatu module, and produces a geth binary.
FROM golang:1.26.5-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /spageth

# Copy the overlay repo (patches, overlay files, scripts).
COPY . .

# Build args select what upstream ref to track and which xatu version to pull.
ARG UPSTREAM_REF=master
ARG XATU_VERSION=latest
ENV UPSTREAM_REF=${UPSTREAM_REF}
ENV XATU_VERSION=${XATU_VERSION}

RUN scripts/spageth-build.sh --ref "${UPSTREAM_REF}"

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /spageth/go-ethereum/build/bin/geth /usr/local/bin/geth

EXPOSE 30303 30303/udp 8545 8546
ENTRYPOINT ["geth"]
