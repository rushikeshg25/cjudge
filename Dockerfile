FROM golang:1.24.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/cjudge ./cmd/cjudge

FROM alpine:3.22 AS api
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
