.PHONY: up down logs seed migrate test clean

up:
	docker compose up

down:
	docker compose down

logs:
	docker compose logs -f $(service)

seed:
	docker compose exec web pnpm prisma db seed

migrate:
	docker compose exec web pnpm prisma migrate dev

migrate-new:
	docker compose exec web pnpm prisma migrate dev --name $(name)

reset-db:
	docker compose exec web pnpm prisma migrate reset --force

test-web:
	cd web && pnpm test

test-proxy:
	cd proxy && go test ./...

test: test-web test-proxy

clean:
	docker compose down -v
	rm -rf web/node_modules web/.next proxy/tmp
