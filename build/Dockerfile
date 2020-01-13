FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

RUN microdnf install -y iproute

ENV OPERATOR=/usr/local/bin/staticroute-operator \
    USER_UID=0 \
    USER_NAME=staticroute-operator

# install operator binary
COPY build/_output/bin/staticroute-operator ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
