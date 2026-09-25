FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cooked ./cmd/cooked

FROM gcr.io/distroless/static:nonroot
COPY --from=build /cooked /cooked
ENV ADDR=:8080 DB_PATH=/data/cooked.db SECURE=1 TRUST_PROXY=1
EXPOSE 8080
ENTRYPOINT ["/cooked"]
