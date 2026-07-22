# syntax=docker/dockerfile:1

# --- builder ---
FROM golang:1.26-alpine AS builder

WORKDIR /src

# cache deps separately from source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tyk-sre-assignment .

# --- runtime ---
# Distroless nonroot: no shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/tyk-sre-assignment /tyk-sre-assignment

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/tyk-sre-assignment"]
