export DOCKER_BUILDKIT=1
GIT_COMMIT  := $(shell git rev-parse --short HEAD)
GIT_BRANCH  := $(shell git symbolic-ref -q --short HEAD)
GIT_SUMMARY := $(shell git describe --tags --dirty --always)
VERSION     := $(shell git describe --tags 2> /dev/null)
BUILD_DATE  := $(shell date +%s)
GIT_COMMIT_FULL  := $(shell git rev-parse HEAD)
DOCKER_IMAGE  := "ghcr.io/metal-toolbox/firmware-syncer"
REPO := "https://github.com/metal-toolbox/firmware-syncer.git"

.DEFAULT_GOAL := help

## Generate mocks
mocks:
	go get go.uber.org/mock@v0.3.0
	go install go.uber.org/mock/mockgen@v0.3.0
	go generate ./...

## Go test
test:
	go test -covermode=atomic ./...

## golangci-lint
lint:
	golangci-lint run --config .golangci.yml --timeout 300s

## Go mod
go-mod:
	go mod tidy -compat=1.19 && go mod vendor

## Build osx bin
build-osx: go-mod
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o firmware-syncer -mod vendor
	sha256sum firmware-syncer > firmware-syncer_checksum.txt

## Build linux bin
build-linux: go-mod
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o firmware-syncer -mod vendor
	sha256sum firmware-syncer > firmware-syncer_checksum.txt

## build docker image and tag as ghcr.io/metal-toolbox/firmware-syncer:latest
build-image: build-linux
	docker build --rm=true -f Dockerfile -t ${DOCKER_IMAGE}:latest . \
		--label org.label-schema.schema-version=1.0 \
		--label org.label-schema.vcs-ref=${GIT_COMMIT_FULL} \
		--label org.label-schema.vcs-url=${REPO}

## tag and push devel docker image to local registry
push-image-devel: build-image
	docker tag ${DOCKER_IMAGE}:latest localhost:5001/firmware-syncer:latest
	docker push localhost:5001/firmware-syncer:latest
	kind load docker-image localhost:5001/firmware-syncer:latest

# https://gist.github.com/prwhite/8168133
# COLORS
GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
WHITE  := $(shell tput -Txterm setaf 7)
RESET  := $(shell tput -Txterm sgr0)

TARGET_MAX_CHAR_NUM=20

## Show help
help:
	@echo ''
	@echo 'Usage:'
	@echo '  ${YELLOW}make${RESET} ${GREEN}<target>${RESET}'
	@echo ''
	@echo 'Targets:'
	@awk '/^[a-zA-Z\-\\_0-9]+:/ { \
		helpMessage = match(lastLine, /^## (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 0, index($$1, ":")-1); \
			helpMessage = substr(lastLine, RSTART + 3, RLENGTH); \
			printf "  ${YELLOW}%-${TARGET_MAX_CHAR_NUM}s${RESET} ${GREEN}%s${RESET}\n", helpCommand, helpMessage; \
		} \
	} \
	{ lastLine = $$0 }' ${MAKEFILE_LIST}
