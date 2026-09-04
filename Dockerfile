FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/latvia-home-radar ./cmd/bot

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 radar
USER radar
COPY --from=build /out/latvia-home-radar /usr/local/bin/latvia-home-radar
ENTRYPOINT ["/usr/local/bin/latvia-home-radar"]
