@echo off
rem 由 service.bat 调用，沿用其 setlocal 与已解析变量。
rem 用法：schtasks.bat install [--user] | uninstall
rem
rem 注意：onstart 与 onlogon 触发器都要求管理员权限，
rem --user 只改变运行身份，不改变权限要求。

set "ACTION=%~1"
set "MODE=%~2"

if /I "%ACTION%"=="install" goto :install
if /I "%ACTION%"=="uninstall" goto :uninstall
echo [eucli] schtasks 未知动作: %ACTION%
exit /b 2

:install
schtasks /query /tn "%EUCLI_SERVICE_NAME%" >nul 2>&1
if not errorlevel 1 (
    echo [eucli] 任务已存在: %EUCLI_SERVICE_NAME%
    exit /b 1
)

net session >nul 2>&1
if errorlevel 1 (
    echo [eucli] 注册开机或登录任务需要管理员权限，请以管理员身份打开命令行后重试。
    exit /b 2
)

set "RUNNER=%~dp0..\run-foreground.bat"

if /I "%MODE%"=="--user" (
    schtasks /create /tn "%EUCLI_SERVICE_NAME%" /tr "\"%RUNNER%\"" /sc onlogon /f >nul
    if errorlevel 1 (
        echo [eucli] 创建登录任务失败。
        exit /b 1
    )
    echo [eucli] 已注册登录任务: %EUCLI_SERVICE_NAME%
    exit /b 0
)

schtasks /create /tn "%EUCLI_SERVICE_NAME%" /tr "\"%RUNNER%\"" /sc onstart /ru SYSTEM /rl HIGHEST /f >nul
if errorlevel 1 (
    echo [eucli] 创建开机任务失败。
    exit /b 1
)
echo [eucli] 已注册开机任务: %EUCLI_SERVICE_NAME%
exit /b 0

:uninstall
schtasks /query /tn "%EUCLI_SERVICE_NAME%" >nul 2>&1
if errorlevel 1 (
    echo [eucli] 任务不存在: %EUCLI_SERVICE_NAME%
    exit /b 0
)
net session >nul 2>&1
if errorlevel 1 (
    echo [eucli] 移除任务需要管理员权限，请以管理员身份打开命令行后重试。
    exit /b 2
)
schtasks /end /tn "%EUCLI_SERVICE_NAME%" >nul 2>&1
schtasks /delete /tn "%EUCLI_SERVICE_NAME%" /f >nul
if errorlevel 1 (
    echo [eucli] 删除任务失败。
    exit /b 1
)
echo [eucli] 已移除任务: %EUCLI_SERVICE_NAME%
exit /b 0
