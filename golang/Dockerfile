# syntax=docker/dockerfile:1

# --- builder ---
FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS builder

WORKDIR /src

# cache deps separately from source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tyk-sre-assignment .

# --- runtime ---
# Distroless nonroot: no shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35

COPY --from=builder /out/tyk-sre-assignment /tyk-sre-assignment

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/tyk-sre-assignment"]
