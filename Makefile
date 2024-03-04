.PHONY: clean build release capscompare

.EXPORT_ALL_VARIABLES:

VERSION = v0.1.1
CGO_ENABLED = 0
SETUID = 1
UPXLVL = -9

all: bin/tunreadwriter bin/sshtun

clean:
	rm -rf bin

build: bin/tunreadwriter bin/sshtun

release:
	cp bin/sshtun bin/sshtun-$(shell go env GOOS)-$(shell go env GOARCH)-$(VERSION)
	cd bin && sha256sum sshtun-$(shell go env GOOS)-$(shell go env GOARCH)-$(VERSION) > checksums.txt

install: bin/tunreadwriter bin/sshtun
	sudo install -m 4755 bin/sshtun /usr/local/sbin/

capslock.json:
	go run github.com/google/capslock/cmd/capslock@latest -output=json > capslock.json

capscompare:
	@O1=$$(go run github.com/google/capslock/cmd/capslock@latest -output=compare capslock.json 2>&1); if [ -n "$$O1" ]; then echo "Capslock changes detected"; echo "$$O1"; exit 1; fi

bin/tunreadwriter: bin
	## tinygo is unfortunately single-threaded and blocks one of the io.Copy goroutines
	#if which tinygo > /dev/null ; then tinygo build -o bin/tunreadwriter -no-debug ./cmd/tunreadwriter ; else go build -o bin/tunreadwriter -ldflags="-s -w" ./cmd/tunreadwriter ; fi
	go build -o bin/tunreadwriter -trimpath -ldflags="-s -w -X main.version=$(VERSION)" ./cmd/tunreadwriter
	strip -s bin/tunreadwriter
	if which upx > /dev/null ; then upx $(UPXLVL) bin/tunreadwriter ; fi

bin/sshtun: bin
	go run golang.org/x/vuln/cmd/govulncheck@latest .
	$(MAKE) capscompare
	go run github.com/securego/gosec/v2/cmd/gosec@latest ./... || true
	trivy repo . --exit-code 1 --scanners vuln,misconfig,secret,license
	go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest app -main ./cmd/sshtun/ -licenses=true -packages=true -json=true -output sshtun.bom.json
	go build -o bin/sshtun -trimpath -ldflags="-s -w -X main.version=$(VERSION)" ./cmd/sshtun
	strip -s bin/sshtun
	if which upx > /dev/null ; then upx $(UPXLVL) bin/sshtun ; fi
ifeq ($(SETUID),1)
	sudo chown root:root bin/sshtun
	sudo chmod 4755 bin/sshtun
endif

bin:
	mkdir bin
