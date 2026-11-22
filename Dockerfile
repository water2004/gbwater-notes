# syntax=docker/dockerfile:1
FROM node:24-bookworm-slim AS node

FROM ubuntu:24.04 AS toolchain
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl gnupg \
    && curl --fail --silent --show-error --location https://apt.llvm.org/llvm-snapshot.gpg.key \
         | gpg --dearmor -o /usr/share/keyrings/llvm.gpg \
    && echo 'deb [signed-by=/usr/share/keyrings/llvm.gpg] https://apt.llvm.org/noble/ llvm-toolchain-noble-21 main' \
         > /etc/apt/sources.list.d/llvm.list \
    && apt-get update && apt-get install -y --no-install-recommends \
         clang-21 clang-tools-21 libc++-21-dev libc++abi-21-dev libunwind-21-dev \
         git pkg-config ninja-build python3-venv build-essential libpango1.0-dev libfontconfig1-dev \
    && rm -rf /var/lib/apt/lists/*
RUN python3 -m venv /opt/cmake && /opt/cmake/bin/pip install --no-cache-dir cmake==4.3.4
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm
ENV PATH=/opt/cmake/bin:$PATH CC=clang-21 CXX=clang++-21

FROM toolchain AS folia
WORKDIR /src
COPY third_party/folia ./third_party/folia
COPY native/folia_bridge ./native/folia_bridge
COPY deploy/prepare-folia.mjs /tmp/prepare-folia.mjs
RUN npm ci --prefix third_party/folia/tools/mathjax-bundle --include=dev --ignore-scripts --no-audit --no-fund \
    && node /tmp/prepare-folia.mjs third_party/folia \
    && cmake -S native/folia_bridge -B /tmp/folia-build -G Ninja \
         -DCMAKE_BUILD_TYPE=Release -DCMAKE_CXX_FLAGS=-stdlib=libc++ \
         -DCMAKE_CXX_STDLIB_MODULES_JSON=/usr/lib/llvm-21/lib/libc++.modules.json \
         -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    && cmake --build /tmp/folia-build --target folia_md FoliaNativeMath FoliaNativeMermaid --parallel 3 \
    && mkdir /out \
    && cp /tmp/folia-build/folia_md /tmp/folia-build/lib/libFoliaNative*.so /out/ \
    && cp -L /usr/lib/llvm-21/lib/libc++.so.1 /usr/lib/llvm-21/lib/libc++abi.so.1 /usr/lib/llvm-21/lib/libunwind.so.1 /out/ \
    && mkdir /out/licenses \
    && cp third_party/folia/LICENSE /out/licenses/Folia-LICENSE \
    && cp third_party/folia/src/mermaid/*LICENSE* third_party/folia/src/mermaid/THIRD_PARTY_NOTICES.md /out/licenses/ \
    && cp /tmp/folia-build/_deps/folia_yaml_cpp-src/LICENSE /out/licenses/YAML-CPP-LICENSE

FROM node AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY internal/app/default_categories.json /src/internal/app/default_categories.json
COPY web/index.html web/vite.config.ts web/tsconfig.json ./
COPY web/src ./src
COPY web/scripts ./scripts
COPY web/public/favicon.svg ./public/favicon.svg
ARG GBWATER_BASE_PATH=/mypdf/
RUN printf '[]\n' > public/catalog.json \
    && GBWATER_BASE_PATH="$GBWATER_BASE_PATH" node scripts/build.mjs \
    && mv dist /src/starter \
    && rm /src/starter/.build-cache.json \
    && printf '%s\n' "$GBWATER_BASE_PATH" > /src/frontend-base

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/gbwater ./cmd/gbwater

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl libpangocairo-1.0-0 fonts-noto-cjk fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 10001 gbwater \
    && useradd --no-create-home -u 10001 -g gbwater gbwater
WORKDIR /app
ENV GBWATER_ROOT=/app GBWATER_ADDR=0.0.0.0:8080 GBWATER_ENV=production \
    GBWATER_FOLIA_BRIDGE=/opt/gbwater/folia/folia_md GBWATER_FOLIA_LIB_DIR=/opt/gbwater/folia \
    LD_LIBRARY_PATH=/opt/gbwater/folia
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=folia /out /opt/gbwater/folia
COPY --from=backend /out/gbwater /usr/local/bin/gbwater
COPY --from=frontend --chown=10001:10001 /src/web /app/web
COPY --from=frontend /src/internal /app/internal
COPY --from=frontend /src/starter /opt/gbwater/frontend
COPY --from=frontend /src/frontend-base /opt/gbwater/frontend-base
COPY --chmod=755 deploy/docker-entrypoint.sh /usr/local/bin/gbwater-entrypoint
RUN install -d -o 10001 -g 10001 /app/content /app/data/content-assets /app/web/dist /app/web/src/generated \
    && chown 10001:10001 /app/data \
    && ln -s ../../data/content-assets /app/web/public/content-assets
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s CMD curl --fail --silent http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["gbwater-entrypoint"]
CMD ["gbwater"]
