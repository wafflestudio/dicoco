FROM golang:1.27.1-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /server \
    ./cmd/bot

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /server /server

ENTRYPOINT ["/server"]
