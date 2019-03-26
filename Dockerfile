FROM golang:latest
ENV GOPATH="/go/"
WORKDIR /go/src/mark
COPY / .
RUN make get
RUN make build

FROM alpine:latest
RUN apk --no-cache add ca-certificates bash
COPY --from=0 /go/src/mark/mark /bin/
RUN mkdir -p /docs
WORKDIR /docs
ENTRYPOINT ["/bin/mark"]
