FROM golang:1.26-trixie AS builder

WORKDIR /app

COPY ./apx/go.mod ./apx/go.sum ./
RUN go mod download

COPY ./apx .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/server .

USER nobody

ENTRYPOINT ["./server"]
