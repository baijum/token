FROM docker.io/library/golang:1.16 AS builder

WORKDIR /go/src/app

COPY . .

RUN make bin

FROM docker.io/library/debian:stable-slim
RUN apt-get update -y && apt-get install ca-certificates -y
WORKDIR /
COPY --from=builder /go/src/app/token .
USER 65532:65532

ENTRYPOINT ["/token"]
