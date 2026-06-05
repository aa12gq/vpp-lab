FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -o /out/vpp-lab ./cmd/vpp-lab

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/vpp-lab /app/vpp-lab
EXPOSE 8080
ENTRYPOINT ["/app/vpp-lab"]
