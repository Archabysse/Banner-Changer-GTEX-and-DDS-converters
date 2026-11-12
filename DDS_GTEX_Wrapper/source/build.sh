#!/bin/sh
# Produit les .exe Windows (64 bits) depuis n'importe quel systeme ayant Go.
# Pour du 32 bits : GOARCH=386
set -e
go test ./...
for t in gtexwrapper ddswrapper2; do
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$t.exe" "./cmd/$t"
done
echo OK
