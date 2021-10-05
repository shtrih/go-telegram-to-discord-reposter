##
## Build
##
FROM golang:1.16-alpine AS build

WORKDIR /app

RUN apk add build-base

COPY . .

RUN go get -d -v ./...
RUN go install -v ./...

RUN go build -o /go-telegram-to-discord-reposter reposter

##
## Deploy
##
FROM alpine

WORKDIR /

COPY --from=build /go-telegram-to-discord-reposter /go-telegram-to-discord-reposter

ENTRYPOINT ["/go-telegram-to-discord-reposter", "--config", "cnf.yaml"]