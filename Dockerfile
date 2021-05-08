FROM docker.io/library/golang:1.16 AS builder

WORKDIR /go/src/app

RUN curl -sLO https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable/openshift-client-linux.tar.gz && tar zxf openshift-client-linux.tar.gz oc

COPY . .

RUN make bin

FROM docker.io/library/debian:stable-slim
RUN apt-get update -y && apt-get install ca-certificates -y
WORKDIR /
COPY --from=builder /go/src/app/token .
COPY --from=builder /go/src/app/oc .
USER 65532:65532

ENTRYPOINT ["/token"]