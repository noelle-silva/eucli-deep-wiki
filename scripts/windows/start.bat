@echo off
chcp 65001 >nul
setlocal EnableExtensions

set "ENV_FILE=%~dp0deploy.env"
if not exist "%ENV_FILE%" (
    echo [eucli] 缺少 deploy.env，请复制 deploy.env.example 后填写。
    exit /b 1
)

for /f "usebackq eol=# tokens=1,* delims==" %%A in ("%ENV_FILE%") do set "%%A=%%B"

for %%V in (EUCLI_DEPLOY_DIR EUCLI_EXE EUCLI_CONFIG EUCLI_LOG_DIR EUCLI_LOG_FILE) do (
    if not defined %%V (
        echo [eucli] deploy.env 缺少 %%V
        exit /b 1
    )
)

set "EXE_PATH=%EUCLI_DEPLOY_DIR%\%EUCLI_EXE%"
if not exist "%EXE_PATH%" (
    echo [eucli] 找不到二进制: %EXE_PATH%
    exit /b 1
)

set "CFG_PATH=%EUCLI_CONFIG%"
if not exist "%CFG_PATH%" set "CFG_PATH=%EUCLI_DEPLOY_DIR%\%EUCLI_CONFIG%"
if not exist "%CFG_PATH%" (
    echo [eucli] 找不到配置: %EUCLI_CONFIG%
    exit /b 1
)

set "LOG_DIR=%EUCLI_LOG_DIR%"
if not exist "%LOG_DIR%" set "LOG_DIR=%EUCLI_DEPLOY_DIR%\%EUCLI_LOG_DIR%"
if not exist "%LOG_DIR%" mkdir "%LOG_DIR%"

set "LOG_FILE=%LOG_DIR%\%EUCLI_LOG_FILE%"

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
