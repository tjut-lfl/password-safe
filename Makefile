.PHONY: build test install tidy clean smoke extensions release

VERSION ?= v0.2.1-beta
LDFLAGS := -X github.com/tjut-lfl/password-safe/internal/cli.Version=$(VERSION)

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

build:
	go build -ldflags "$(LDFLAGS)" -o bin/password-safe ./cmd/password-safe

test:
	go test ./...

tidy:
	go mod tidy

install: build
	install -d $(BINDIR)
	install -m 755 bin/password-safe $(BINDIR)/password-safe

clean:
	rm -rf bin/ dist/

# 非交互冒烟（使用临时目录）
smoke: build
	@tmpdir=$$(mktemp -d) && \
	export PASSWORD_SAFE_VAULT="$$tmpdir/vault.dat" PASSWORD_SAFE_MASTER='smoke-test-master-99' && \
	./bin/password-safe init && \
	./bin/password-safe add --name work/github --username alice --password 's3cret!' --url https://github.com --tags dev --totp JBSWY3DPEHPK3PXP && \
	./bin/password-safe list --query github && \
	./bin/password-safe search github && \
	./bin/password-safe tree && \
	./bin/password-safe get work/github --json | head -c 200 && echo && \
	./bin/password-safe totp work/github && \
	./bin/password-safe generate -l 16 && \
	./bin/password-safe generate --memorable --words 4 && \
	./bin/password-safe verify && \
	./bin/password-safe status && \
	./bin/password-safe export -o "$$tmpdir/backup.dat" && \
	echo 'name,username,password,url,notes,tags' > "$$tmpdir/in.csv" && \
	echo 'mail,a@b.com,pw,https://m,n,work' >> "$$tmpdir/in.csv" && \
	./bin/password-safe import -f "$$tmpdir/in.csv" && \
	./bin/password-safe list && \
	echo "SMOKE OK (vault=$$tmpdir)"

extensions:
	VERSION=$(VERSION) bash scripts/build-extensions.sh

release:
	VERSION=$(VERSION) bash scripts/build-release.sh
