#!/bin/bash

# skip tests if it is non-empty, can be any value
if [[ ! -z "$SKIP_TESTS" ]]; then
    echo "skipping tests by request"
    exit 0
fi

if [[ ! -v BUILDPLATFORM ]]; then
    CGO_ENABLED=0 go test -v ./...
elif [[ "$BUILDPLATFORM" == "$TARGETPLATFORM" ]]; then
    echo "testing in docker"
    CGO_ENABLED=${CGO_ENABLED} GOOS=${TARGETOS} GOARCH=${TARGETARCH} go test -v ./...
else
    echo "skipping tests during cross builds"
fi