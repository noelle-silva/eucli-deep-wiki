@echo off
chcp 65001 >nul
setlocal EnableExtensions

call "%~dp0lib\load-env.bat"
if errorlevel 1 exit /b 1

rem 判重只认服务端程序：与注册成任务或服务时的判据是同一个名字。
call "%~dp0lib\is-running.bat"
if not errorlevel 1 (
    echo [eucli] %EUCLI_SERVER_EXE% 已在运行，跳过启动。
    exit /b 0
)

echo [eucli] 启动 %EUCLI_SERVER_EXE%
echo [eucli] 配置 %CFG_PATH%
echo [eucli] 日志 %LOG_FILE%

rem 隐藏窗口拉起服务端，stdout 与 stderr 分两个文件落盘。
rem 启动参数取自 SERVER_ARGS：与前台执行体、服务注册用的是同一份。
powershell -NoProfile -WindowStyle Hidden -Command "Start-Process -FilePath '%EXE_PATH%' -ArgumentList '%SERVER_ARGS%' -WorkingDirectory '%EUCLI_DEPLOY_DIR%' -WindowStyle Hidden -RedirectStandardOutput '%LOG_FILE%.out' -RedirectStandardError '%LOG_FILE%'"
if errorlevel 1 (
    echo [eucli] 启动失败: %EXE_PATH%
    exit /b 1
)

exit /b 0
