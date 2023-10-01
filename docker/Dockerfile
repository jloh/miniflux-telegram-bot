# Use the Go image to build our application.
FROM golang:1.21-alpine as builder

ARG litestream_version="0.3.11"
# This arg comes from the buildx environment and changes
# depending on what arch is being built
ARG TARGETARCH

# Download the static build of Litestream directly into the path & make it executable.
# This is done in the builder and copied as the chmod doubles the size.
ADD https://github.com/benbjohnson/litestream/releases/download/v${litestream_version}/litestream-v${litestream_version}-linux-${TARGETARCH}.tar.gz /tmp/litestream.tar.gz
RUN tar -C /usr/local/bin -xzf /tmp/litestream.tar.gz

FROM golang:1.21-alpine

# Add in Litestream
COPY --from=builder /usr/local/bin/litestream /usr/local/bin/litestream

# Add in required packages
RUN apk --no-cache add ca-certificates tzdata

# Finally add in app
COPY miniflux_bot /app/miniflux_bot

COPY ./docker/start /app/start

# Make sure script is executable
RUN chmod +x /app/start

LABEL org.opencontainers.image.title="Miniflux Telegram Bot"
LABEL org.opencontainers.image.description="A Telegram Bot to sync new Miniflux entries with"
LABEL org.opencontainers.image.vendor="James Loh"
LABEL org.opencontainers.image.licenses="MIT License"
LABEL org.opencontainers.image.url=https://github.com/jloh/miniflux-telegram-bot
LABEL org.opencontainers.image.source=https://github.com/jloh/miniflux-telegram-bot

WORKDIR /app
ENTRYPOINT ["/app/start"]
