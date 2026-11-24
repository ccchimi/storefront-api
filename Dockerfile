FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
# The SQLite driver is pure Go, so the binary is fully static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/storefront ./cmd/storefront \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/storefront /storefront
# The image runs as the nonroot user (65532), so the data directory has to
# belong to it; otherwise SQLite can't create the database file.
COPY --from=build --chown=65532:65532 /out/data /data
ENV ADDR=:8000 DB_PATH=/data/storefront.db
VOLUME /data
EXPOSE 8000
ENTRYPOINT ["/storefront"]
