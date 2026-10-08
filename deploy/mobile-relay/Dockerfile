FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOEXPERIMENT=greenteagc go build -trimpath -ldflags="-s -w" -o /atlas-mobile-relay ./cmd/atlas-mobile-relay

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /atlas-mobile-relay /atlas-mobile-relay
USER nonroot:nonroot
ENTRYPOINT ["/atlas-mobile-relay"]
