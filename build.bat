@echo off
REM Compilează VideoEncoder.exe (necesită Go 1.22+ de la https://go.dev/dl/)
cd /d "%~dp0"
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w" -o VideoEncoder.exe .
if errorlevel 1 (echo EROARE la compilare & pause & exit /b 1)
echo Gata: VideoEncoder.exe
pause
