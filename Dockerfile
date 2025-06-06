FROM golang:1.24.4 AS builder
ENV GOPATH="/go"
WORKDIR /go/src/github.com/kovetskiy/mark
COPY / .
RUN make get \
&& make build

FROM chromedp/headless-shell:latest
RUN apt-get update \
&& apt-get upgrade -qq \
&& apt-get install --no-install-recommends -qq ca-certificates bash sed git dumb-init \
&& apt-get clean \
&& rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

COPY --from=builder /go/src/github.com/kovetskiy/mark/mark /bin/
WORKDIR /docs

ENTRYPOINT ["dumb-init", "--"]
