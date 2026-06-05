.PHONY: run test tidy docker-up docker-down

run:
	go run ./cmd/vpp-lab

test:
	go test ./...

tidy:
	go mod tidy

docker-up:
	docker compose up -d

docker-down:
	docker compose down
