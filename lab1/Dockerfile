FROM golang:1.23-alpine as build

RUN apk add build-base

# set COPY destination
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy all necessary directories
ADD ./ ./

# Build the Go binary
RUN CGO_ENABLED=1 GOOS=linux go build -o /video_rec_server ./video_rec_service/server/server.go

FROM alpine:latest as deploy
COPY --from=build /video_rec_server /
EXPOSE 8080

ENTRYPOINT ["/video_rec_server"]
