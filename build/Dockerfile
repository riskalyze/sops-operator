FROM registry.access.redhat.com/ubi8/ubi-minimal

RUN microdnf update -y && \
    microdnf install -y shadow-utils && \
    rm -rf /var/cache/yum && \
    microdnf clean all

RUN groupadd --gid 1000 sops-operator && \
    useradd --uid 1000 --gid 1000 sops-operator

RUN curl -fsSLo /usr/local/bin/sops https://github.com/getsops/sops/releases/download/v3.13.3/sops-v3.13.3.linux.amd64 && \
    echo "e5bec3346a873ae91d871550f3e698c1aad962aff462a080e40f25fde17fef6b  /usr/local/bin/sops" | sha256sum --check && \
    chmod +x /usr/local/bin/sops

USER sops-operator
WORKDIR /home/sops-operator

COPY sops-operator /usr/local/bin/

ENTRYPOINT ["sops-operator"]
