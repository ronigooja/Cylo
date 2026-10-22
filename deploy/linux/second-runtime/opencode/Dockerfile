FROM node:22-bookworm-slim@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates python3 \
    && rm -rf /var/lib/apt/lists/*

COPY package.json package-lock.json /opt/opencode/
RUN npm ci --prefix /opt/opencode --omit=dev --ignore-scripts \
    && ln -s /opt/opencode/node_modules/.bin/opencode /usr/local/bin/opencode \
    && npm cache clean --force

COPY proxy_bridge.py /usr/local/bin/cylo-opencode-proxy-bridge
RUN chmod 0555 /usr/local/bin/cylo-opencode-proxy-bridge
ENTRYPOINT ["/usr/local/bin/cylo-opencode-proxy-bridge"]
