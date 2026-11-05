# Not the checkout's directory name: .gitignore covers /mark, and the
# Dockerfile copies the binary from there whatever the directory is called.
NAME = mark

# The tag HEAD is exactly at. Anywhere else the version is left unset, so that
# Go's pseudo-version names the commit rather than an older tag.
VERSION = $(shell git describe --tags --exact-match 2>/dev/null)
# "+dirty" when the tree has changes, untracked files too, as go build reports it.
DIRTY = $(if $(shell git status --porcelain 2>/dev/null),+dirty)
COMMIT = $(shell git rev-parse HEAD 2>/dev/null)$(DIRTY)

# The Git remote make release pushes its tag to.
GIT_REMOTE = origin

.PHONY: build test release clean

build:
	@echo :: building go binary $(or $(VERSION),$(COMMIT))
	CGO_ENABLED=0 go build -trimpath \
		-ldflags "$(if $(VERSION),-X main.version=$(VERSION)) $(if $(COMMIT),-X main.commit=$(COMMIT))" \
		-o $(NAME) \
		./cmd/mark

test:
	go test -race -coverprofile=profile.cov ./... -v

# Tags HEAD and pushes that one tag; CI builds the images and GoReleaser the
# release from it. Neither git tag nor git push without -f moves a tag that
# exists, so a release cannot rewrite one already out. Only a RELEASE given
# on the command line counts, so a stray one in the environment tags nothing.
release:
	$(if $(and $(filter command line,$(origin RELEASE)),$(RELEASE)),,$(error name the new tag: make release RELEASE=vX.Y.Z))
	git tag $(RELEASE)
	git push $(GIT_REMOTE) refs/tags/$(RELEASE)

clean:
	rm -rf $(NAME)
