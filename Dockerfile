FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN go build -o /out/eucli-dw ./cmd/eucli-dw

FROM alpine:3.22

RUN apk add --no-cache ca-certificates git openssh-client tzdata \
    && git config --system core.longpaths true \
    && git config --system safe.directory '*'

WORKDIR /app

COPY --from=build /out/eucli-dw /usr/local/bin/eucli-dw

EXPOSE 9997

ENTRYPOINT ["eucli-dw"]
CMD ["serve", "--service-config", "/app/config.json"]
