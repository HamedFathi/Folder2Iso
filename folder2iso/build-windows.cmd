@echo off
setlocal
cd /d "%~dp0"
set CGO_ENABLED=0
go test ./...
if errorlevel 1 exit /b 1
go build -buildvcs=false -trimpath -ldflags="-s -w" -o folder2iso.exe .
if errorlevel 1 exit /b 1
echo Built folder2iso.exe
