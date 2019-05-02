#!/bin/sh

export DB_USER=cornel
export DB_PASS=xxx
export DB_HOST=
export DB_DB=
export HOST=
export PORT=

export UPLOAD_DIR=./upload

#go run multipleUploads.go
./gw-manual-uploads

