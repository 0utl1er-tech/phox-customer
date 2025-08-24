DB_URL=postgresql://root:secret@localhost:5432/prism?sslmode=disable

postgres:
	docker run --name postgres -p 5432:5432 -e POSTGRES_USER=root -e POSTGRES_PASSWORD=secret -d postgres:17-alpine

createdb:
	docker exec -it postgres createdb --username=root --owner=root prism

dropdb:
	docker exec -it postgres dropdb prism --username=root 

migrateup:
	migrate -path db/migration -database "$(DB_URL)" -verbose up

migrate%:
	migrate -path db/migration -database "$(DB_URL)" -verbose up $*

migratedown:
	migrate -path db/migration -database "$(DB_URL)" -verbose down

new_migration:
	migrate create -ext sql -dir db/migration -seq $(name)

force:
	migrate -path db/migration -database "$(DB_URL)" -verbose force 0

db_docs:
	dbdocs build docs/phox-customer.dbml

db_schema:
	dbml2sql --postgres -o docs/schema.sql docs/phox-customer.dbml
	cp docs/schema.sql db/migration/000000_init_schema.up.sql

sqlc:
	sqlc generate

test:
	go test -v -cover -short ./...

server:
	go run main.go

proto:
	rm -f docs/swagger/*.swagger.json
	rm -rf gen/pb/*
	buf generate
	statik -src=./docs/swagger -dest=./docs

evans:
	evans --host localhost --port 9090 -r repl

redis:
	docker run --name redis -p 6379:6379 -d redis:7-alpine

.PHONY: network postgres createdb dropdb migrateup migratedown migrateup1 migratedown1 new_migration db_docs db_schema sqlc test server mock proto evans redis