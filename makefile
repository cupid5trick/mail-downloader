install:
	go get -t -v -u ./...

linter:
	golangci-lint run