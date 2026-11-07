# syntax=docker/dockerfile:1
FROM golang:1.26.8 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/hydroledger-server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hydroledger-server /hydroledger-server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/hydroledger-server"]
