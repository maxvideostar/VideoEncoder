@echo off
REM Deschide portul VideoEncoder în Windows Firewall (rulează ca Administrator)
set PORT=8090
if not "%~1"=="" set PORT=%~1
net session >nul 2>&1 || (echo Ruleaza acest fisier cu click dreapta - "Run as administrator" & pause & exit /b 1)
netsh advfirewall firewall delete rule name="VideoEncoder" >nul 2>&1
netsh advfirewall firewall add rule name="VideoEncoder" dir=in action=allow protocol=TCP localport=%PORT%
echo Portul %PORT% este deschis pentru VideoEncoder.
pause
