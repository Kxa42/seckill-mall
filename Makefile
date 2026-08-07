GO ?= go
PROTOC ?= protoc
GO_TMPDIR ?= /tmp/seckill-mall-go-build
GO_CACHE ?= /tmp/seckill-mall-go-cache

.PHONY: fmt fmt-check generate boundaries list test vet build e2e check

fmt:
	gofmt -w services shared tools

fmt-check:
	@test -z "$$(gofmt -l services shared tools)" || (gofmt -l services shared tools && exit 1)

generate:
	cd shared/proto/commerce && $(PROTOC) -I . \
		--go_out=../../gen/commerce --go_opt=paths=source_relative \
		--go-grpc_out=../../gen/commerce --go-grpc_opt=paths=source_relative \
		*.proto

boundaries:
	bash scripts/check_boundaries.sh

list:
	GOTMPDIR=$(GO_TMPDIR) GOCACHE=$(GO_CACHE) $(GO) list ./...

test:
	GOTMPDIR=$(GO_TMPDIR) GOCACHE=$(GO_CACHE) $(GO) test ./...

vet:
	GOTMPDIR=$(GO_TMPDIR) GOCACHE=$(GO_CACHE) $(GO) vet ./...

build:
	GOTMPDIR=$(GO_TMPDIR) GOCACHE=$(GO_CACHE) $(GO) build ./services/... ./tools/...

e2e:
	bash tests/stage5_memory_e2e.sh

check: fmt-check boundaries list test vet build e2e
