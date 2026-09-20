@echo off
rem Runs fake-spot-adb.ps1 in place of adb.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0fake-spot-adb.ps1" %*
exit /b %ERRORLEVEL%
