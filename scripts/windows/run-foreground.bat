@echo off
chcp 65001 >nul
setlocal EnableExtensions

rem 前台运行并落日志。任务计划程序与服务包装器都用它当执行体，
rem 这样进程生命周期与任务状态一致，日志也不会丢。

call "%~dp0lib\load-env.bat"
if errorlevel 1 exit /b 1

rem 固定工作目录，避免继承调用方路径、把某个无关目录一直占住。
cd /d "%EUCLI_DEPLOY_DIR%"

"%EXE_PATH%" serve --service-config "%CFG_PATH%" >> "%LOG_FILE%" 2>&1
