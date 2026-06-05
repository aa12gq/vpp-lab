.PHONY: run test tidy smoke docker-up docker-down

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

docker-down:
	docker compose down
