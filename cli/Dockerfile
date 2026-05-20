# rubezh CLI — Go binary для общения с API из терминала.
# Multi-arch билд; результат — статический бинарь, удобный для docker run.

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY rubezh-api/go.mod rubezh-api/go.sum ./rubezh-api/
RUN cd rubezh-api && go mod download
COPY rubezh-api ./rubezh-api
RUN cd rubezh-api && CGO_ENABLED=0 go build -o /out/rubezh ./cmd/rubezh

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/rubezh /usr/local/bin/rubezh
ENTRYPOINT ["/usr/local/bin/rubezh"]
