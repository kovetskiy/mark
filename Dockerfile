FROM golang:latest
ENV GOPATH="/go"
WORKDIR /go/src/github.com/kovetskiy/mark
COPY / .
RUN make get
RUN make build

FROM chromedp/headless-shell:latest
RUN apt-get update -y \
    && apt-get install -y ca-certificates bash git dumb-init \
    && apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
COPY --from=0 /go/src/github.com/kovetskiy/mark/mark /bin/
ENTRYPOINT ["dumb-init", "--"]
RUN mkdir /docs
WORKDIR /docs
