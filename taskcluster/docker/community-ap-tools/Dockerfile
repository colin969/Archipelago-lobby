FROM rust:1.92-slim-trixie AS builder

RUN apt update && \
    apt install -y libssl-dev pkg-config && \
    rm -rf /var/lib/{apt,dpkg,cache,log}/

WORKDIR /app

COPY ./community-ap-tools ./
COPY Cargo.lock ./
COPY ./common /common

RUN cargo build --release

FROM debian:13-slim

RUN apt update && \
    apt install -y ca-certificates openssl curl && \
    rm -rf /var/lib/{apt,dpkg,cache,log}/

WORKDIR /app

COPY --from=builder /app/community-ap-tools/target/release/community-ap-tools ./

CMD ["/app/community-ap-tools"]
