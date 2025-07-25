all: lint test
PHONY: test coverage lint golint clean vendor ci-test

PG_STRING=postgres://postgres:postgres@pg:5432
DB_NAME=governor
DEV_DB=${PG_STRING}/${DB_NAME}_dev?sslmode=disable
TEST_DB=${PG_STRING}/${DB_NAME}_test?sslmode=disable

GOOS=linux
DB_STRING=host=localhost port=5432 user=root sslmode=disable
DB_STRING_DC=host=crdb port=5432 user=root sslmode=disable
TEST_DB_DC=${DB_STRING_DC} dbname=${DB_NAME}_test

# OAuth client generated secret
SECRET := $(shell bash -c 'openssl rand -hex 16')

dev-serve:
	go run . serve --config .governor-dev.yaml

lint: golint

golint: | vendor
	@echo Linting Go files...
	@golangci-lint run --build-tags "-tags testtools"

vendor:
	@go mod download
	@go mod tidy

# Setup an OAuth server and dev OAuth client
# We replace 3000 with 3333 below to not have any port collions with the governor default ui port
# The sleep exists to let hydra come up for lack of a better mechanism to ensure hydra is ready
dev-hydra: |
	@if [ ! -f "hydra/hydra.yml" ]; then \
		mkdir -p hydra; \
		curl -s -o hydra/hydra.yml "https://raw.githubusercontent.com/ory/hydra/master/contrib/quickstart/5-min/hydra.yml"; \
		sed -i -e 's/3000/3333/g' hydra/hydra.yml; \
		sed -i -e 's/http:\/\/127.0.0.1:4444/http:\/\/hydra:4444/g' hydra/hydra.yml; \
		echo "strategies:\n  access_token: jwt\n" >> ./hydra/hydra.yml; \
		echo "oidc:\n  subject_identifiers:\n    supported_types:\n      - public\n" >> ./hydra/hydra.yml; \
	fi;

	@echo creating hydra client-id governor and client-secret ${SECRET}
	@hydra clients create \
		--endpoint http://hydra:4445/ \
		--audience http://api:3001/ \
		--id governor \
		--secret ${SECRET} \
		--grant-types client_credentials \
		--response-types token,code \
		--token-endpoint-auth-method client_secret_post \
		--scope  write,read
	@echo "\nYour client \"governor\" was generated with password \"${SECRET}\""
	@echo "\nYou can fetch a JWT token like so:\n"
	@echo "hydra token client \\"
	@echo "  --endpoint http://hydra:4444/ \\"
	@echo "  --client-id governor \\"
	@echo "  --client-secret ${SECRET} \\"
	@echo "  --audience http://api:3001/ \\"
	@echo "  --scope write,read"
	@echo

test-database: | vendor
	psql ${PG_STRING} -c "drop database if exists ${DB_NAME}_test"
	psql ${PG_STRING} -c "create database ${DB_NAME}_test"
	GOVERNOR_DB_URI="${TEST_DB}" go run main.go migrate up

generate-models:
	$(MAKE) test-database
	sqlboiler --add-soft-deletes psql

test:
	@echo Running unit tests...
	@go test -cover -short -tags testtools ./...

coverage:
	@echo Generating coverage report...
	go test ./... -race -coverprofile=coverage.out -covermode=atomic -tags testtools -p 1
	@go tool cover -func=coverage.out
	@go tool cover -html=coverage.out

build:
	@go mod download
	@CGO_ENABLED=0 GOOS=linux go build -mod=readonly -v -o governor-api

clean:
	@echo Cleaning...
	@rm -rf ./dist/
	@rm -rf coverage.out
	@rm -f governor-api
	@go clean -testcache

ci-test: | test
	go run main.go migrate up
