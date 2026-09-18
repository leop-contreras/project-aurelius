FROM golang:1.27-alpine AS build
WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN go build -o load-balancer main.go

FROM alpine:latest
WORKDIR /app
COPY --from=build /app/load-balancer .
EXPOSE 9000
CMD ["./load-balancer"]