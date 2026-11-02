@echo off
REM Instalare completa VideoEncoder (Intel QSV / AMD AMF / NVIDIA NVENC)
REM Parametri optionali: -Dir C:\VideoEncoder -Port 8090 -Password PAROLA -NoService -Silent
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0install.ps1" %*
