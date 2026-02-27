.PHONY: dev build test clean deploy

dev:
	go run ./cmd/server/

build:
	go build -o fast-unli ./cmd/server/

test:
	go test ./... -v

clean:
	rm -f fast-unli *.db

deploy:
	fly deploy
