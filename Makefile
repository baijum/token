
.PHONY: build
build:
	podman build . -t quay.io/baijum/token:latest

.PHONY: push
push:
	podman push quay.io/baijum/token:latest

.PHONY: bin
bin:
	go get -d -v ./...
	go install -v ./...
	go build -o token main.go
