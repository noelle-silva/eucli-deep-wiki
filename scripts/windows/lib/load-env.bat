@echo off
rem 读取并校验 deploy.env，产出 EXE_PATH / CFG_PATH / LOG_FILE 等派生变量。
rem 约定：调用方已 setlocal，本文件不再 setlocal，变量向上传递。

set "ENV_FILE=%~dp0..\deploy.env"
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

if not defined EUCLI_SERVICE_NAME set "EUCLI_SERVICE_NAME=eucli-deep-wiki"

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

exit /b 0
