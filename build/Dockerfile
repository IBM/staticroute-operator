FROM debian:stretch AS builder

RUN apt-get update && apt-get install -y libcap2-bin

COPY build/_output/bin/staticroute-operator /staticroute-operator
RUN setcap cap_net_admin+ep /staticroute-operator
RUN chmod go+x /staticroute-operator

FROM scratch
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /staticroute-operator /staticroute-operator

USER 2000:2000
CMD ["/staticroute-operator"]
