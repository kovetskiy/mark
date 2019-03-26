FROM golang:latest
ENV GOPATH="/go/"
ENV GO111MODULE="on"
WORKDIR /go/src/mark
COPY / .
RUN make get
RUN make build

FROM alpine:latest
RUN apk --no-cache add ca-certificates bash
WORKDIR /
COPY --from=0 /go/src/mark/mark /bin/
ENTRYPOINT ["/bin/mark"]
