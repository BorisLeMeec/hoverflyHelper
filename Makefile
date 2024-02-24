.PHONY: build

build:
	GOARCH=arm64 go build -o hoverflyTester
	chmod +x hoverflyTester
