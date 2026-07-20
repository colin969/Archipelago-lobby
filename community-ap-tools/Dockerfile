FROM rust:1.92-slim-trixie AS builder

RUN apt update && \
    apt install -y libssl-dev pkg-config && \
    rm -rf /var/lib/{apt,dpkg,cache,log}/

WORKDIR /app

ARG PROFILE=release

COPY Cargo.lock ./
COPY ./community-ap-tools/Cargo.toml ./community-ap-tools/Cargo.toml
COPY ./common ./common

# Cache deps so building isn't as slow later
RUN mkdir -p community-ap-tools/src && \
    echo "fn main() {}" > community-ap-tools/src/main.rs

RUN cargo build $([ "$PROFILE" = "release" ] && echo "--release") --manifest-path community-ap-tools/Cargo.toml && \
    rm -rf community-ap-tools/src

# Do actual build
COPY --exclude=community-ap-tools/static ./community-ap-tools ./community-ap-tools

RUN cargo build $([ "$PROFILE" = "release" ] && echo "--release") --manifest-path community-ap-tools/Cargo.toml

# Move to final image without all the build tools
FROM debian:13-slim

RUN apt update && \
    apt install -y ca-certificates openssl curl && \
    rm -rf /var/lib/{apt,dpkg,cache,log}/

WORKDIR /app

ARG PROFILE=release

COPY --from=builder /app/community-ap-tools/target/${PROFILE}/community-ap-tools ./app
COPY ./community-ap-tools/static ./community-ap-tools/static

CMD ["/app/app"]
