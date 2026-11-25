# Not the checkout's directory name: .gitignore covers /mark, and the
# Dockerfile copies the binary from there whatever the directory is called.
NAME = mark

# The tag HEAD is exactly at. Anywhere else the version is left unset, so that
# Go's pseudo-version names the commit rather than an older tag.
VERSION = $(shell git describe --tags --exact-match 2>/dev/null)
# "+dirty" when the tree has changes, untracked files too, as go build reports it.
DIRTY = $(if $(shell git status --porcelain 2>/dev/null),+dirty)
COMMIT = $(shell git rev-parse HEAD 2>/dev/null)$(DIRTY)

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
# release from it. The tag goes to GIT_REMOTE, which is origin unless the
# command line or the environment names another:
#
#	make release RELEASE=vX.Y.Z GIT_REMOTE=upstream
#
# Only a RELEASE given on the command line counts, so a stray one in the
# environment tags nothing. It must be a semantic version, which also keeps a
# space or a leading "-" from reaching git as an option such as -f. The shell
# reads both values from the environment, where make puts them, so that a
# quote in one is not shell syntax, and "--" keeps git from taking the remote
# for an option.
#
# HEAD must be the tip of the remote's master with nothing changed, so that
# the tag names what is there and not a commit or a tree only this clone has.
#
# A tag the remote has is refused before one is made here: pushing a tag that
# is already there at the same commit succeeds and releases nothing. Neither
# git tag nor git push without -f moves a tag that exists. A push that fails
# takes the new tag back off, so the clone neither keeps a tag the remote
# lacks nor refuses the same command run again.
release: export GIT_REMOTE ?= origin
release:
	$(if $(filter environment%,$(origin RELEASE)),$(error the RELEASE in the environment is not used; name the new tag on the command line: make release RELEASE=vX.Y.Z))
	$(if $(and $(filter command line,$(origin RELEASE)),$(RELEASE)),,$(error name the new tag on the command line: make release RELEASE=vX.Y.Z))
	@if printf '%s\n' "$$RELEASE" | grep -Evqx 'v$(MAJOR)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-$(PRE)(\.$(PRE))*)?'; then \
		echo "make release: RELEASE=$$RELEASE is not a version of this module: write v$(MAJOR).Y.Z, or v$(MAJOR).Y.Z-rc.1 for a pre-release, with no leading zero in a number and no +build part" >&2; exit 1; fi
	@if [ -z "$$GIT_REMOTE" ]; then \
		echo "make release: GIT_REMOTE is empty; name the remote the tag goes to, or leave it unset for origin" >&2; exit 1; fi
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "make release: the tree has changes or untracked files; commit or remove them first, since the tag carries none of them" >&2; exit 1; fi
	@git fetch --quiet --no-tags -- "$$GIT_REMOTE" refs/heads/master || { \
		echo "make release: cannot fetch master from $$GIT_REMOTE" >&2; exit 1; }
	@if [ "$$(git rev-parse HEAD)" != "$$(git rev-parse 'FETCH_HEAD^{commit}')" ]; then \
		echo "make release: HEAD is not the tip of master on $$GIT_REMOTE; check that commit out (git checkout master && git pull --ff-only $$GIT_REMOTE master), push what it lacks, or name the remote to release from with GIT_REMOTE=" >&2; exit 1; fi
	@git ls-remote --exit-code --tags -- "$$GIT_REMOTE" "refs/tags/$$RELEASE" >/dev/null; case $$? in \
		2) ;; \
		0) echo "make release: $$GIT_REMOTE already has the tag $$RELEASE, and a tag is never moved; name the next version" >&2; exit 1;; \
		*) echo "make release: cannot list the tags of $$GIT_REMOTE" >&2; exit 1;; esac
	git tag $(RELEASE)
	git push -- "$$GIT_REMOTE" refs/tags/$(RELEASE) || { git tag -d $(RELEASE); exit 1; }

clean:
	rm -rf $(NAME)
