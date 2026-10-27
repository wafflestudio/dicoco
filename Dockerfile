FROM golang:1.27.1-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /server \
    ./cmd/bot

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git bash tar gzip libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

# CI changes this value on each build so BuildKit checks the official latest
# release again instead of reusing the previous OpenCode installation layer.
ARG OPENCODE_INSTALL_REVISION=local
RUN test -n "$OPENCODE_INSTALL_REVISION" \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod 644 /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && printf 'deb [arch=%s signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main\n' "$(dpkg --print-architecture)" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL https://opencode.ai/v2/install -o /tmp/install-opencode.sh \
    && bash /tmp/install-opencode.sh --no-modify-path \
    && cp /root/.opencode/bin/opencode /usr/local/bin/opencode \
    && rm -rf /root/.opencode /tmp/install-opencode.sh \
    && useradd --uid 10001 --create-home bot \
    && mkdir -p /data /workspace/dicoco \
    && chown bot:bot /data

COPY --from=build /server /server
COPY --chown=root:root go.mod go.sum /workspace/dicoco/
COPY --chown=root:root cmd /workspace/dicoco/cmd
COPY --chown=root:root internal /workspace/dicoco/internal
COPY --chown=root:root AGENTS.md README.md /workspace/dicoco/

ENV OPENCODE_DIRECTORY=/workspace/dicoco \
    XDG_DATA_HOME=/data/opencode/data \
    XDG_STATE_HOME=/data/opencode/state \
    XDG_CONFIG_HOME=/data/opencode/config \
    XDG_CACHE_HOME=/data/opencode/cache

USER bot
WORKDIR /workspace/dicoco

ENTRYPOINT ["/server"]
