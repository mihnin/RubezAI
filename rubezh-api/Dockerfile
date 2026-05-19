# rubezh-api — API Gateway «Рубеж ИИ».
# Multi-stage: сборка статического бинарника, образ — distroless (без shell).

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/rubezh-api ./cmd/rubezh-api

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/rubezh-api /rubezh-api
EXPOSE 8080
# HEALTHCHECK вызывает сам бинарник (distroless не содержит shell/curl).
ENTRYPOINT ["/rubezh-api"]
