# syntax=docker/dockerfile:1

# ######################
# # Build image
# ######################
ARG GO_VERSION=1.22.2
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
WORKDIR /src

# Cache dependencies
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x

# Build
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,target=. \
    CGO_ENABLED=0 GOARCH="$TARGETARCH" go build -o /bin/server .


# ######################
# # Final image
# ######################
FROM alpine:3.18.3 AS final

# Install ca-certificates and timezone data
RUN --mount=type=cache,target=/var/cache/apk \
    apk --update add \
        ca-certificates \
        tzdata \
        && \
        update-ca-certificates

# Create a non-root user
ARG UID=10001
RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "${UID}" \
    appuser
USER appuser

# Copy the binary
COPY --from=build /bin/server /bin/

# Expose the port
EXPOSE 4200

# Run the binary
ENTRYPOINT [ "/bin/server" ]
