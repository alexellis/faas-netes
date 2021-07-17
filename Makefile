.PHONY: build local push namespaces install charts start-kind stop-kind build-buildx render-charts
TAG?=latest
OWNER?=openfaas
export DOCKER_CLI_EXPERIMENTAL=enabled

all: build-docker

local:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o faas-netes

build-docker:
	docker build \
	-t ghcr.io/$(OWNER)/faas-netes:$(TAG) .

.PHONY: build-buildx
build-buildx:
	@echo ghcr.io/$(OWNER)/faas-netes:$(TAG) && \
	docker buildx create --use --name=multiarch --node=multiarch && \
	docker buildx build \
		--push \
		--platform linux/amd64 \
		--tag ghcr.io/$(OWNER)/faas-netes:$(TAG) \
		.

.PHONY: build-buildx-all
build-buildx-all:
	@docker buildx create --use --name=multiarch --node=multiarch && \
	docker buildx build \
		--platform linux/amd64,linux/arm/v7,linux/arm64 \
		--output "type=image,push=false" \
		--tag ghcr.io/$(OWNER)/faas-netes:$(TAG) \
		.

.PHONY: publish-buildx-all
publish-buildx-all:
	@echo  ghcr.io/$(OWNER)/faas-netes:$(TAG) && \
	docker buildx create --use --name=multiarch --node=multiarch && \
	docker buildx build \
		--platform linux/amd64,linux/arm/v7,linux/arm64 \
		--push=true \
		--tag ghcr.io/$(OWNER)/faas-netes:$(TAG) \
		.

push:
	docker push ghcr.io/$(OWNER)/faas-netes:$(TAG)

namespaces:
	kubectl apply -f namespaces.yml

install: namespaces
	kubectl apply -f yaml/

charts:
	cd chart && helm package openfaas/ && helm package kafka-connector/ && helm package cron-connector/ && helm package nats-connector/ && helm package mqtt-connector/
	mv chart/*.tgz docs/
	helm repo index docs --url https://openfaas.github.io/faas-netes/ --merge ./docs/index.yaml
	./contrib/create-static-manifest.sh

render-charts:
	./contrib/create-static-manifest.sh
	./contrib/create-static-manifest.sh ./chart/openfaas ./yaml_arm64 ./chart/openfaas/values-arm64.yaml
	./contrib/create-static-manifest.sh ./chart/openfaas ./yaml_armhf ./chart/openfaas/values-armhf.yaml

start-kind: ## attempt to start a new dev environment
	@./contrib/create_dev.sh

stop-kind: ## attempt to stop the dev environment
	@./contrib/stop_dev.sh
