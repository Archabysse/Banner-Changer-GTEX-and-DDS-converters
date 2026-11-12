@echo off
rem Necessite Go (https://go.dev). Produit gtexwrapper.exe et ddswrapper2.exe
go test ./... || exit /b 1
go build -trimpath -ldflags="-s -w" -o gtexwrapper.exe ./cmd/gtexwrapper || exit /b 1
go build -trimpath -ldflags="-s -w" -o ddswrapper2.exe ./cmd/ddswrapper2 || exit /b 1
echo OK
