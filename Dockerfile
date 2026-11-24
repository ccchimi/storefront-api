FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The SQLite driver is pure Go, so the binary is fully static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/storefront ./cmd/storefront

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/storefront /storefront
ENV ADDR=:8000 DB_PATH=/data/storefront.db
VOLUME /data
EXPOSE 8000
ENTRYPOINT ["/storefront"]
