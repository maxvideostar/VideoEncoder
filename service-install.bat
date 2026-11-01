@echo off
REM Instaleaza VideoEncoder ca serviciu Windows (porneste automat cu Windows)
REM Optional cont propriu: service-install.bat NumeUtilizator Parola
net session >nul 2>&1 && goto :admin
if "%~1"=="" (powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs") else (powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -ArgumentList '%~1','%~2' -Verb RunAs")
exit /b
:admin
cd /d "%~dp0"
taskkill /im VideoEncoder.exe /f >nul 2>&1
if "%~1"=="" (
  VideoEncoder.exe -install
) else (
  VideoEncoder.exe -install -svc-user "%~1" -svc-pass "%~2"
)
echo.
echo Interfata: http://127.0.0.1:8090/
pause
