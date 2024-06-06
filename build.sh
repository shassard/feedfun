#!/bin/sh

set -ex

env GOOS=linux CGO_ENABLED=0 go build -o feedfun cmd/cli/main.go
