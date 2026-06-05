FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -o /out/vpp-lab ./cmd/vpp-lab
RUN go build -o /out/simulator ./cmd/simulator
RUN go build -o /out/edge-gateway ./cmd/edge-gateway

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/vpp-lab /app/vpp-lab
COPY --from=build /out/simulator /app/simulator
COPY --from=build /out/edge-gateway /app/edge-gateway
EXPOSE 8080
ENTRYPOINT ["/app/vpp-lab"]
