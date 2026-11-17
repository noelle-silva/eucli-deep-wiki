@echo off
chcp 65001 >nul
setlocal EnableExtensions

call "%~dp0lib\load-env.bat"
if errorlevel 1 exit /b 1

tasklist /FI "IMAGENAME eq %EUCLI_EXE%" /NH 2>nul | findstr /I /C:"%EUCLI_EXE%" >nul
if not errorlevel 1 (
    echo [eucli] %EUCLI_EXE% 已在运行，跳过启动。
    exit /b 0
)

echo [eucli] 启动 %EUCLI_EXE%
echo [eucli] 配置 %CFG_PATH%
echo [eucli] 日志 %LOG_FILE%
powershell -NoProfile -WindowStyle Hidden -Command "Start-Process -FilePath '%EXE_PATH%' -ArgumentList '-config','%CFG_PATH%' -WindowStyle Hidden -RedirectStandardOutput '%LOG_FILE%.out' -RedirectStandardError '%LOG_FILE%'"
exit /b 0
