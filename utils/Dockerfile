ARG GO_IMAGE=mirror.gcr.io/library/golang:1.27-alpine
ARG BASE_IMAGE=mirror.gcr.io/library/alpine:3.24.3
ARG CONTROLLER_BASE_IMAGE=gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

# =============================================================================
# Stage 1: Build the Go binaries (the CLI and the controller)
# =============================================================================
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS builder

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w \
      -X github.com/this-is-tobi/homelab/utils/cmd.version=${VERSION} \
      -X github.com/this-is-tobi/homelab/utils/cmd.commit=${COMMIT} \
      -X github.com/this-is-tobi/homelab/utils/cmd.date=${BUILD_DATE}" \
    -o /ohmlab .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w \
      -X github.com/this-is-tobi/homelab/utils/internal/buildinfo.Version=${VERSION} \
      -X github.com/this-is-tobi/homelab/utils/internal/buildinfo.Commit=${COMMIT} \
      -X github.com/this-is-tobi/homelab/utils/internal/buildinfo.Date=${BUILD_DATE}" \
    -o /ohmlab-controller ./cmd/controller

# =============================================================================
# Stage 2: Controller image (target `controller`)
# Static binary and CA certificates only: no shell, no package manager. The
# base runs as the numeric user 65532, which `runAsNonRoot` needs.
#
# This stage sits BEFORE the toolbox on purpose: an image built without a
# target is the last stage, and that must stay the toolbox.
# =============================================================================
FROM ${CONTROLLER_BASE_IMAGE} AS controller

COPY --from=builder /ohmlab-controller /ohmlab-controller

USER 65532:65532
ENTRYPOINT ["/ohmlab-controller"]

# =============================================================================
# Stage 3: Final image (toolbox) - the default target
# =============================================================================
FROM ${BASE_IMAGE}

ARG TARGETARCH
ARG USERNAME=ohmlab
ARG USER_UID=1000
ARG USER_GID=${USER_UID}
# Third-party tool versions — pinned, checksum-verified below. Bump
# deliberately (kubectl tracks the cluster's k3s minor).
ARG VAULT_VERSION=2.2.1
ARG KUBECTL_VERSION=v1.36.4
ARG HELM_VERSION=v4.3.1

ENV HOME=/home/${USERNAME}

WORKDIR ${HOME}

# Install toolbox packages (kept for ad-hoc use; Go binary handles automated tasks)
RUN apk add --no-cache \
    bash \
    ca-certificates \
    curl \
    git \
    gzip \
    jq \
    openssl \
    tar \
    yq \
    && rm -rf /var/cache/apk/*

# Install Vault CLI (still useful for manual operations) — verified against
# the published SHA256SUMS so the build is reproducible and tamper-evident.
RUN curl -fsSL "https://releases.hashicorp.com/vault/${VAULT_VERSION}/vault_${VAULT_VERSION}_linux_${TARGETARCH}.zip" -o "vault_${VAULT_VERSION}_linux_${TARGETARCH}.zip" \
    && curl -fsSL "https://releases.hashicorp.com/vault/${VAULT_VERSION}/vault_${VAULT_VERSION}_SHA256SUMS" -o vault_SHA256SUMS \
    && grep "vault_${VAULT_VERSION}_linux_${TARGETARCH}.zip" vault_SHA256SUMS | sha256sum -c - \
    && unzip -o "vault_${VAULT_VERSION}_linux_${TARGETARCH}.zip" vault -d /usr/bin/ \
    && chmod +x /usr/bin/vault \
    && rm "vault_${VAULT_VERSION}_linux_${TARGETARCH}.zip" vault_SHA256SUMS

# Install kubectl — pinned (tracks the cluster's k3s minor) + checksum-verified.
RUN curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" -o /usr/local/bin/kubectl \
    && curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl.sha256" -o kubectl.sha256 \
    && echo "$(cat kubectl.sha256)  /usr/local/bin/kubectl" | sha256sum -c - \
    && chmod +x /usr/local/bin/kubectl \
    && rm kubectl.sha256

# Install helm — pinned release tarball + checksum, instead of piping an
# unpinned script from the `main` branch into a shell.
RUN curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" -o "helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" \
    && curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz.sha256sum" -o helm.sha256sum \
    && sha256sum -c helm.sha256sum \
    && tar -xzf "helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" -C /usr/local/bin --strip-components=1 "linux-${TARGETARCH}/helm" \
    && chmod +x /usr/local/bin/helm \
    && rm "helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" helm.sha256sum

# Create non-root user
RUN addgroup -g ${USER_GID} ${USERNAME} \
    && adduser -D -h ${HOME} -s /bin/bash -G ${USERNAME} -u ${USER_UID} ${USERNAME} \
    && chown -R ${USERNAME}:root ${HOME} \
    && chgrp -R 0 ${HOME} \
    && chmod -R g=u ${HOME}

# Copy Go binary
COPY --from=builder --chown=${USER_UID}:${USER_GID} /ohmlab /usr/local/bin/ohmlab

# Ensure binary is executable
RUN chmod +x /usr/local/bin/ohmlab

# Numeric UID, not the account name: Kubernetes cannot resolve a named user
# at admission, so `runAsNonRoot: true` without an explicit runAsUser rejects
# pods running this image ("container has runAsNonRoot and image has
# non-numeric user"). The require-non-root policy in this repo enforces that.
USER ${USER_UID}

ENTRYPOINT ["ohmlab"]
CMD ["--help"]
