@echo off
rem Lets a script that runs "adb" run fake-adb.ps1 instead.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0fake-adb.ps1" %*
exit /b %ERRORLEVEL%
