FROM golang:1.27.3 AS builder
ENV GOPATH="/go"
WORKDIR /go/src/github.com/kovetskiy/mark
# Modules first, so a source-only change reuses the downloaded dependencies.
COPY go.mod go.sum ./
RUN go mod download
COPY / .
RUN make get \
&& make build

FROM chromedp/headless-shell:latest

ARG TARGETARCH

RUN apt-get update \
&& apt-get upgrade -qq \
&& apt-get install --no-install-recommends -qq ca-certificates bash sed git curl dumb-init xz-utils \
&& apt-get clean \
&& rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# The version mark itself requires, rather than one written down again here: the
# same file is embedded in the binary, so the image cannot install a merman the
# code then refuses. Copied after the apt step above, which clears /tmp.
COPY --from=builder /go/src/github.com/kovetskiy/mark/mermaid/merman-version.txt /etc/merman-version

# The optional native mermaid renderer, selected with --mermaid-engine=merman,
# for the two architectures the image is built for. Any other goes without it
# and draws with Chrome, which is the default anyway -- and mark says so plainly
# if merman is asked for and is not there.
RUN set -eux; \
    case "${TARGETARCH}" in \
        amd64) arch=x86_64 ;; \
        arm64) arch=aarch64 ;; \
        *) arch= ;; \
    esac; \
    if [ -n "${arch}" ]; then \
        MERMAN_VERSION="$(cat /etc/merman-version)"; \
        base="https://github.com/Latias94/merman/releases/download/v${MERMAN_VERSION}"; \
        archive="merman-cli-${arch}-unknown-linux-gnu.tar.xz"; \
        cd /tmp; \
        curl -fsSL -o "${archive}" "${base}/${archive}"; \
        curl -fsSL -o "${archive}.sha256" "${base}/${archive}.sha256"; \
        sha256sum -c "${archive}.sha256"; \
        tar -xJf "${archive}"; \
        install -m 0755 "merman-cli-${arch}-unknown-linux-gnu/merman-cli" /usr/local/bin/merman-cli; \
        merman-cli --version; \
        rm -rf /tmp/merman-cli-*; \
    fi

COPY --from=builder /go/src/github.com/kovetskiy/mark/mark /bin/
WORKDIR /docs

ENTRYPOINT ["dumb-init", "--"]
