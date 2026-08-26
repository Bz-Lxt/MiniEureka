.PHONY: fmt test test-race vet build frontend-test frontend-build verify compose-config up down logs

fmt:
	cd backend && gofmt -w $$(find . -name '*.go' -type f)

test:
	cd backend && go test ./...

test-race:
	cd backend && go test -race ./...

vet:
	cd backend && go vet ./...

build:
	cd backend && go build ./...

frontend-test:
	cd frontend-admin && npm test -- --run

frontend-build:
	cd frontend-admin && npm run build

verify: test test-race vet build frontend-test frontend-build compose-config

compose-config:
	docker compose config --quiet

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs --tail=100 -f

