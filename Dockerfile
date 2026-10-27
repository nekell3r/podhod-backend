FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /podhod ./cmd/podhod

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 podhod && useradd --uid 10001 --gid 10001 --no-create-home podhod
COPY --from=build /podhod /usr/local/bin/podhod
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/podhod"]
