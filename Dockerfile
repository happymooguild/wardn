# ---- build ----
FROM golang:1.26 AS build
WORKDIR /src

# Cache modules first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static binary so it runs in a scratch/distroless image.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/wardn-backend .

# ---- run ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/wardn-backend /wardn-backend
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/wardn-backend"]
