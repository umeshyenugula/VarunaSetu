# syntax=docker/dockerfile:1
FROM golang:1.26.8 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/hydroledger-server ./cmd/server \
 && CGO_ENABLED=0 go build -o /out/citysim ./cmd/citysim

# Live virtual-city demo (docker build --target citysim). It embeds its own API
# on a virtual clock, so it does not use the separate api service.
FROM gcr.io/distroless/static-debian12:nonroot AS citysim
COPY --from=build /out/citysim /citysim
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/citysim"]
CMD ["serve", "-addr", "0.0.0.0:8090"]

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hydroledger-server /hydroledger-server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/hydroledger-server"]
