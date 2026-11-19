@echo off
chcp 65001 >nul
setlocal EnableExtensions

rem 服务管理入口，只负责校验参数与分发实现。
rem 用法：
rem   service.bat install schtasks [--user]
rem   service.bat install nssm
rem   service.bat uninstall schtasks|nssm
rem   service.bat status

set "ACTION=%~1"
set "ROUTE=%~2"
set "MODE=%~3"

call "%~dp0lib\load-env.bat"
if errorlevel 1 exit /b 1

if /I "%ACTION%"=="status" goto :status

if /I not "%ACTION%"=="install" if /I not "%ACTION%"=="uninstall" (
    call :usage
    exit /b 2
)

if /I "%ROUTE%"=="schtasks" goto :dispatch
if /I "%ROUTE%"=="nssm" goto :dispatch

echo [eucli] 请指定路线: schtasks 或 nssm
call :usage
exit /b 2

:dispatch
call "%~dp0impl\%ROUTE%.bat" "%ACTION%" "%MODE%"
exit /b %errorlevel%

:status
echo [eucli] 服务名 %EUCLI_SERVICE_NAME%
schtasks /query /tn "%EUCLI_SERVICE_NAME%" >nul 2>&1
if errorlevel 1 (
    echo [eucli] 任务计划: 未注册
) else (
    echo [eucli] 任务计划: 已注册
)
sc query "%EUCLI_SERVICE_NAME%" >nul 2>&1
if errorlevel 1 (
    echo [eucli] Windows 服务: 未注册
) else (
    echo [eucli] Windows 服务: 已注册
)
call "%~dp0lib\is-running.bat"
if errorlevel 1 (
    echo [eucli] 进程: 未运行
) else (
    echo [eucli] 进程: 运行中
)
exit /b 0

:usage
echo 用法:
echo   service.bat install schtasks [--user]   注册任务计划，默认开机以 SYSTEM 运行
echo   service.bat install nssm                注册 Windows 服务，需已安装 nssm
echo   service.bat uninstall schtasks^|nssm    移除已注册项
echo   service.bat status                      查看注册与运行状态
exit /b 0
