# syntax=docker/dockerfile:1

# --- builder ---
FROM golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder

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
