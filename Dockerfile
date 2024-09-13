# Builder stage
ARG BUILDER_IMAGE
ARG INTERMEDIATE_IMAGE
FROM $BUILDER_IMAGE AS builder
ENV GO111MODULE=on
WORKDIR /
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY version/ version/
ARG ARCH
ARG CGO
ARG BUILDPARAM
RUN CGO_ENABLED=${CGO} GOOS=linux GOARCH=${ARCH} go build ${GOBUILDFLAGS} -o /staticroute-operator main.go

# Intermediate stage to apply capabilities
FROM $INTERMEDIATE_IMAGE AS intermediate

RUN apt-get update && apt-get install -y libcap2-bin
COPY --from=builder /staticroute-operator /staticroute-operator
RUN setcap cap_net_admin+ep /staticroute-operator
RUN chmod go+x /staticroute-operator

# Final image
FROM scratch

COPY --from=intermediate /staticroute-operator /staticroute-operator
USER 2000:2000

ENTRYPOINT ["/staticroute-operator"]
