.PHONY: run test tidy smoke docker-up docker-edge docker-down

run:
	go run ./cmd/vpp-lab

test:
	go test ./...

tidy:
	go mod tidy

smoke:
	./scripts/smoke.sh

docker-up:
	docker compose up -d

docker-edge:
	docker compose --profile edge up -d edge-gateway

docker-down:
	docker compose down
