# Dockerfile for webhookd.
#
# This file is consumed by GoReleaser's dockers_v2 pipeline, not by a
# plain `docker build` from a source checkout. GoReleaser stages the
# already-built binaries under $TARGETPLATFORM/ in a temporary build
# context, so this file copies them rather than compiling anything.
#
# A plain `docker build -t webhookd .` from a source checkout will fail
# with "COPY failed: no source files were specified" — the staged
# binary does not exist until GoReleaser has run. To produce the image
# locally, use:
#
#     goreleaser release --snapshot --clean
#
# which runs the same pipeline without publishing.

FROM scratch

# TARGETPLATFORM is provided by buildx when the image is built with
# --platform. GoReleaser's dockers_v2 block passes linux/amd64 and
# linux/arm64, so this resolves to one of those two strings and matches
# the per-platform directory GoReleaser created in the build context.
ARG TARGETPLATFORM

COPY $TARGETPLATFORM/webhookd /webhookd

# Port 8080 is the default the binary binds. Note that the listener's
# default host is 127.0.0.1, so a container launched without --host
# 0.0.0.0 will not accept connections from outside the container. The
# README's docker run examples pass the subcommand but not --host; that
# is a known gap between the current README and the safe default, and
# it will be reconciled when the README is next revised.
EXPOSE 8080

ENTRYPOINT ["/webhookd"]
