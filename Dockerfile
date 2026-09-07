FROM golang:1.27.1 AS builder
ENV GOPATH="/go"
WORKDIR /go/src/github.com/kovetskiy/mark
COPY / .
RUN make get \
&& make build

FROM chromedp/headless-shell:latest

# The optional native mermaid renderer, selected with --mermaid-engine=merman.
# Pinned, because it decides how diagrams are drawn: 0.8.0-alpha.6 is the oldest
# mark accepts, and the checksum is the release's own.
ARG MERMAN_VERSION=0.8.0-alpha.6
ARG TARGETARCH

RUN apt-get update \
&& apt-get upgrade -qq \
&& apt-get install --no-install-recommends -qq ca-certificates bash sed git curl dumb-init xz-utils \
&& apt-get clean \
&& rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# amd64 only, which is where merman publishes a Linux build. An arm64 image goes
# without it and draws with Chrome, which is the default anyway -- and mark says
# so plainly if merman is asked for and is not there.
RUN set -eux; \
    if [ "${TARGETARCH}" = "amd64" ]; then \
        base="https://github.com/Latias94/merman/releases/download/v${MERMAN_VERSION}"; \
        archive="merman-cli-x86_64-unknown-linux-gnu.tar.xz"; \
        cd /tmp; \
        curl -fsSL -o "${archive}" "${base}/${archive}"; \
        curl -fsSL -o "${archive}.sha256" "${base}/${archive}.sha256"; \
        sha256sum -c "${archive}.sha256"; \
        tar -xJf "${archive}"; \
        install -m 0755 merman-cli-x86_64-unknown-linux-gnu/merman-cli /usr/local/bin/merman-cli; \
        merman-cli --version; \
        rm -rf /tmp/merman-cli-*; \
    fi

COPY --from=builder /go/src/github.com/kovetskiy/mark/mark /bin/
WORKDIR /docs

ENTRYPOINT ["dumb-init", "--"]
