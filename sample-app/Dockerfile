# ---- build ----
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/sample-app .

# ---- run ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sample-app /sample-app
USER nonroot:nonroot
ENTRYPOINT ["/sample-app"]
