FROM golang:1.26.8-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=1.0.0
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/cjudge ./cmd/cjudge

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS api
RUN apk add --no-cache ca-certificates
COPY --from=build /out/cjudge /usr/local/bin/cjudge
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["cjudge"]
CMD ["api"]

FROM api AS worker
USER root
RUN apk add --no-cache docker-cli
USER 65532:65532
CMD ["worker"]
