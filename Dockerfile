FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN go build -o /out/reference-sync ./cmd/reference-sync \
    && go build -o /out/reference-add ./cmd/reference-add

FROM alpine:3.22

RUN apk add --no-cache ca-certificates git openssh-client tzdata \
    && git config --system core.longpaths true \
    && git config --system safe.directory '*'

WORKDIR /app

COPY --from=build /out/reference-sync /usr/local/bin/reference-sync
COPY --from=build /out/reference-add /usr/local/bin/reference-add

EXPOSE 9997

ENTRYPOINT ["reference-sync"]
CMD ["-config", "/app/config.json"]
