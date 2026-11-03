# check=skip=InvalidDefaultArgInFrom
# The image of AI Gude. GoReleaser builds it with buildx and hands this build only the binaries it compiled
# (one folder per platform), so there is no compile step here. See .goreleaser.yaml.
#
# FFMPEG_VERSION is the ffmpeg of mise.toml; `task release` and `task release:snapshot` read it from there and
# GoReleaser passes it on. It has no default on purpose: a build without it must fail rather than pick a version.
ARG FFMPEG_VERSION
ARG BASE_IMAGE=gcr.io/distroless/static-debian13:nonroot

# A static ffmpeg for amd64 and arm64. AI Gude only pipes PCM through it into OGG/Opus.
FROM mwader/static-ffmpeg:${FFMPEG_VERSION} AS ffmpeg

# Only CA certificates, no shell and no package manager. The time zones are embedded in the binary.
FROM ${BASE_IMAGE}

# Folders are mounted at run time (the runtime creates the mount points) and whoever mounts them gives them to the
# user 10001 (the Ansible collection under deploy/ does that), so the image prepares and owns none.
ENV AIGUDE_USERS_DIR=/etc/aigude/users
ENV AIGUDE_DATA_DIR=/var/lib/aigude/data
# The service listens on all addresses of the container; publish the port only where it should be reachable.
ENV AIGUDE_LISTEN_ADDR=0.0.0.0:8080
EXPOSE 8080
# The working directory is where an aigude.toml is looked for, next to the users folder.
WORKDIR /etc/aigude
USER 10001:10001

COPY --from=ffmpeg /ffmpeg /usr/local/bin/ffmpeg

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/aigude /usr/local/bin/aigude

ENTRYPOINT ["/usr/local/bin/aigude"]
CMD ["serve"]
