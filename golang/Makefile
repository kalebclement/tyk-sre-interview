APP       := tyk-sre-assignment
IMAGE     := tyk-sre-assignment:dev

.PHONY: build test vet fmt lint scan helm-lint run clean

build:
	go build -o $(APP) .

test:
	go test -count=1 -v ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

lint: vet fmt
	govulncheck ./...

scan: build
	docker build -t $(IMAGE) .
	trivy image --severity CRITICAL,HIGH $(IMAGE)

helm-lint:
	helm lint helm/tyk-sre-tool/

run: build
	./$(APP) --address :8080

clean:
	rm -f $(APP)
