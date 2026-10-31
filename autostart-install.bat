@echo off
REM Pornește VideoEncoder automat la logare (fără fereastră de browser)
set EXE=%~dp0VideoEncoder.exe
set LNK=%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\VideoEncoder.lnk
powershell -NoProfile -Command "$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%LNK%');$s.TargetPath='%EXE%';$s.Arguments='-nogui';$s.WorkingDirectory='%~dp0';$s.WindowStyle=7;$s.Save()"
echo Pornire automata activata: %LNK%
pause
