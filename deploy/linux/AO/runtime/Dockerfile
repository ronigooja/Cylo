FROM node:22-bookworm-slim@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d

ARG CODEX_VERSION=0.146.0
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates python3 \
    && rm -rf /var/lib/apt/lists/* \
    && npm install --global "@openai/codex@${CODEX_VERSION}" \
    && npm cache clean --force

COPY proxy_bridge.py /usr/local/bin/cylo-codex-proxy-bridge
RUN chmod 0555 /usr/local/bin/cylo-codex-proxy-bridge \
    && mkdir -p /run/cylo
ENTRYPOINT ["/usr/local/bin/cylo-codex-proxy-bridge"]
