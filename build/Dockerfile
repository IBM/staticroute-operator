FROM debian:stretch AS builder

RUN apt-get update && apt-get install -y libcap2-bin

ENV USER=appuser
ENV UID=10001
ENV GID=10001

RUN adduser \    
    --disabled-password \    
    --gecos "" \    
    --home "/nonexistent" \    
    --shell "/sbin/nologin" \    
    --no-create-home \    
    --uid "${UID}" \ 
    "${USER}"

COPY build/_output/bin/staticroute-operator /staticroute-operator
RUN setcap cap_net_admin+ep /staticroute-operator
RUN chmod go+x /staticroute-operator

FROM scratch
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /staticroute-operator /staticroute-operator

USER 10001:10001
CMD ["/staticroute-operator"]
