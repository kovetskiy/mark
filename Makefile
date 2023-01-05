NAME = $(notdir $(PWD))

VERSION = $(shell git describe --tags --abbrev=0)

GO111MODULE = on

REMOTE = kovetskiy

version:
	@echo $(VERSION)

get:
	go get -v -d

build:
	@echo :: building go binary $(VERSION)
	CGO_ENABLED=0 go build \
		-ldflags "-X main.version=$(VERSION)" \
		-gcflags "-trimpath $(GOPATH)/src"

test:
	go test -race -coverprofile=profile.cov ./...

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

release: image push
	git tag -f $(VERSION)
	git push --tags

clean:
	rm -rf $(NAME)
