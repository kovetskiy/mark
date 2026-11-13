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
# The major version in the module path, which a release tag must carry: Go
# will not serve v18.0.0 of a module whose path ends in /v17.
MAJOR = $(or $(shell sed -n 's|^module .*/v\([0-9][0-9]*\)[[:space:]]*$$|\1|p' go.mod),[01])
# One dot-separated part of a pre-release: a number without a leading zero,
# or anything with a letter or "-" in it. Go serves no tag that breaks this.
PRE = (0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)

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
# It must be a semantic version, which also keeps a space or a leading "-"
# from reaching git as an option such as -f; the check reads it from the
# environment, where make puts it, so that a quote in it is not shell syntax.
# A push that fails takes the new tag back off, so the clone neither keeps a
# tag the remote may have elsewhere nor refuses the same command run again.
release:
	$(if $(and $(filter command line,$(origin RELEASE)),$(RELEASE)),,$(error name the new tag: make release RELEASE=vX.Y.Z))
	@if printf '%s\n' "$$RELEASE" | grep -Evqx 'v$(MAJOR)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-$(PRE)(\.$(PRE))*)?'; then \
		echo "make release: RELEASE=$$RELEASE is not v$(MAJOR).Y.Z, the module path's major version" >&2; exit 1; fi
	git tag $(RELEASE)
	git push $(GIT_REMOTE) refs/tags/$(RELEASE) || { git tag -d $(RELEASE); exit 1; }

clean:
	rm -rf $(NAME)
