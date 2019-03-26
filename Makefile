NAME = $(notdir $(PWD))

VERSION = $(shell printf "%s.%s" \
	$$(git rev-list --count HEAD) \
	$$(git rev-parse --short HEAD) \
)

GO111MODULE = off

BRANCH = $(shell git rev-parse --abbrev-ref HEAD)

REMOTE = kovetskiy

version:
	@echo $(VERSION)

get:
	go get -v -d

build:
	@echo :: building go binary $(VERSION)
	CGO_ENABLED=0 GOOS=linux go build \
		-ldflags "-X main.version=$(VERSION)" \
		-gcflags "-trimpath $(GOPATH)/src"

image:
	@echo :: building image $(NAME):$(VERSION)
	@docker build -t $(NAME):$(VERSION) -f Dockerfile .
	docker tag $(NAME):$(VERSION) $(NAME):latest

push:
	$(if $(REMOTE),,$(error REMOTE is not set))
	$(eval VERSION ?= latest)
	$(eval TAG ?= $(REMOTE)/$(NAME):$(VERSION))
	@echo :: pushing image $(TAG)
	@docker tag $(NAME):$(VERSION) $(TAG)
	@docker push $(TAG)
	@docker tag $(NAME):$(VERSION) $(REMOTE)/$(NAME):latest
	@docker push $(REMOTE)/$(NAME):latest

clean:
	rm -rf $(NAME)
