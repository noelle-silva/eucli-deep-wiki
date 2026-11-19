@echo off
rem 读取并校验 deploy.env，产出派生变量供调用方直接使用：
rem   EXE_PATH     服务端程序的绝对路径
rem   CFG_PATH     配置文件的绝对路径
rem   LOG_FILE     日志文件的绝对路径（日志目录不存在时创建）
rem   SERVER_ARGS  服务端启动参数，全套脚本里唯一的定义处
rem 约定：调用方已 setlocal，本文件不再 setlocal，变量向上传递。

set "ENV_FILE=%~dp0..\deploy.env"
if not exist "%ENV_FILE%" (
    echo [eucli] 缺少 deploy.env，请复制 deploy.env.example 后填写。
    exit /b 1
)

for /f "usebackq eol=# tokens=1,* delims==" %%A in ("%ENV_FILE%") do set "%%A=%%B"

for %%V in (EUCLI_DEPLOY_DIR EUCLI_SERVER_EXE EUCLI_CONFIG EUCLI_LOG_DIR EUCLI_LOG_FILE) do (
    if not defined %%V (
        echo [eucli] deploy.env 缺少 %%V
        exit /b 1
    )
)

if not defined EUCLI_SERVICE_NAME set "EUCLI_SERVICE_NAME=eucli-deep-wiki"

rem 服务端程序名是纯文件名：位置由部署目录推出，名字用于进程判重。
for %%C in ("%EUCLI_SERVER_EXE%") do if not "%%~nxC"=="%%~C" (
    echo [eucli] EUCLI_SERVER_EXE 只填文件名，不带目录: %EUCLI_SERVER_EXE%
    exit /b 1
)

set "EXE_PATH=%EUCLI_DEPLOY_DIR%\%EUCLI_SERVER_EXE%"
if not exist "%EXE_PATH%" (
    echo [eucli] 找不到服务端程序: %EXE_PATH%
    exit /b 1
)
call :absolute EXE_PATH "%EXE_PATH%"

rem 配置与日志的路径都分两段解析：先按原样（可写绝对路径，也可写相对当前目录），
rem 找不到再按部署目录解析。定下来的都转成绝对路径，之后再切工作目录也不影响结果。

set "CFG_PATH=%EUCLI_CONFIG%"
if not exist "%CFG_PATH%" set "CFG_PATH=%EUCLI_DEPLOY_DIR%\%EUCLI_CONFIG%"
if not exist "%CFG_PATH%" (
    echo [eucli] 找不到配置: %EUCLI_CONFIG%
    exit /b 1
)
call :absolute CFG_PATH "%CFG_PATH%"

set "LOG_DIR=%EUCLI_LOG_DIR%"
if not exist "%LOG_DIR%" set "LOG_DIR=%EUCLI_DEPLOY_DIR%\%EUCLI_LOG_DIR%"
if not exist "%LOG_DIR%" mkdir "%LOG_DIR%"
if not exist "%LOG_DIR%" (
    echo [eucli] 无法创建日志目录: %LOG_DIR%
    exit /b 1
)
call :absolute LOG_DIR "%LOG_DIR%"
set "LOG_FILE=%LOG_DIR%\%EUCLI_LOG_FILE%"

rem 服务端没有任何子命令，直接带它自己的参数启动；参数只在这里写一次。
set "SERVER_ARGS=-config "%CFG_PATH%""

exit /b 0

:absolute
rem 把相对路径按当前目录补全成绝对路径，结果写回第一个参数指定的变量。
set "%~1=%~f2"
exit /b 0
