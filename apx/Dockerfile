FROM rust:1.92-slim-trixie AS builder

RUN apt update && \
    apt install -y libssl-dev pkg-config && \
    rm -rf /var/lib/{apt,dpkg,cache,log}/

WORKDIR /app

COPY Cargo.toml Cargo.lock ./
COPY apx/Cargo.toml ./apx/

COPY ./apx ./

# Cache deps
RUN mkdir -p apx/src && echo "fn main() {}" > apx/src/main.rs && \
    cargo build --release -p apx && \
    rm -rf apx/src

COPY apx/src ./apx/src

RUN cargo build --release

FROM debian:13-slim

RUN apt update && \
    apt install -y ca-certificates openssl && \
    rm -rf /var/lib/{apt,dpkg,cache,log}/

WORKDIR /app

COPY --from=builder /app/target/release/apx ./

CMD ["/app/apx"]
