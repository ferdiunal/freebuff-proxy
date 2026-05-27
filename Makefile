# ## Kullanım örneği
#
# ```bash
# make docker
# DOCKER_IMAGE=registry.example.com/freebuff-proxy:dev make docker
# ```
DOCKER_IMAGE ?= freebuff-proxy:local
PROXY_URL ?= http://127.0.0.1:1455
PROXY_API_KEY ?= local-proxy-key
SMOKE_MODEL ?= deepseek/deepseek-v4-pro

.PHONY: test build run run-bin doctor smoke-openai smoke-anthropic fmt vet docker

test:
	go test ./...

build:
	go build -o bin/freebuff-proxy ./cmd/freebuff-proxy

run:
	go run ./cmd/freebuff-proxy serve

run-bin: build
	./bin/freebuff-proxy serve

doctor:
	@echo "Port 1455 listener:"
	@lsof -nP -iTCP:1455 -sTCP:LISTEN || true
	@if [ -f ./freebuff-proxy ]; then \
		echo "Uyarı: ./freebuff-proxy root binary mevcut; stale artifact olabilir."; \
		if strings ./freebuff-proxy | grep -F upstream_chat_route_not_verified >/dev/null; then \
			echo "Uyarı: ./freebuff-proxy eski upstream_chat_route_not_verified stringini içeriyor."; \
		fi; \
	else \
		echo "./freebuff-proxy root binary bulunamadı."; \
	fi
	@if [ -f ./bin/freebuff-proxy ]; then \
		if strings ./bin/freebuff-proxy | grep -F upstream_chat_route_not_verified >/dev/null; then \
			echo "Uyarı: ./bin/freebuff-proxy eski upstream_chat_route_not_verified stringini içeriyor."; \
		else \
			echo "./bin/freebuff-proxy eski hata stringini içermiyor."; \
		fi; \
	else \
		echo "./bin/freebuff-proxy bulunamadı; make build çalıştırın."; \
	fi

smoke-openai:
	curl -sS -X POST "$(PROXY_URL)/v1/chat/completions" \
		-H "Authorization: Bearer $(PROXY_API_KEY)" \
		-H "Content-Type: application/json" \
		-d '{"model":"$(SMOKE_MODEL)","messages":[{"role":"user","content":"Merhaba, bana tek cümleyle kendini tanıtır mısın?"}],"stream":false}'

smoke-anthropic:
	curl -sS -X POST "$(PROXY_URL)/v1/messages" \
		-H "x-api-key: $(PROXY_API_KEY)" \
		-H "anthropic-version: 2023-06-01" \
		-H "Content-Type: application/json" \
		-d '{"model":"$(SMOKE_MODEL)","max_tokens":256,"messages":[{"role":"user","content":"Merhaba, bana tek cümleyle kendini tanıtır mısın?"}],"stream":false}'

fmt:
	gofmt -w .

vet:
	go vet ./...

docker:
	docker build -t $(DOCKER_IMAGE) .
