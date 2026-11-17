@echo off
rem 由 service.bat 调用，沿用其 setlocal 与已解析变量。
rem 用法：nssm.bat install | uninstall
rem 依赖：nssm 在 PATH 中，或 deploy.env 里指定 EUCLI_NSSM。

set "ACTION=%~1"

set "NSSM_EXE=%EUCLI_NSSM%"
if not defined NSSM_EXE set "NSSM_EXE=nssm"
where "%NSSM_EXE%" >nul 2>&1
if errorlevel 1 (
    echo [eucli] 未找到 nssm。请安装后加入 PATH，或在 deploy.env 指定 EUCLI_NSSM。
    exit /b 2
)

if /I "%ACTION%"=="install" goto :install
if /I "%ACTION%"=="uninstall" goto :uninstall
echo [eucli] nssm 未知动作: %ACTION%
exit /b 2

:install
sc query "%EUCLI_SERVICE_NAME%" >nul 2>&1
if not errorlevel 1 (
    echo [eucli] 服务已存在: %EUCLI_SERVICE_NAME%
    exit /b 1
)

"%NSSM_EXE%" install "%EUCLI_SERVICE_NAME%" "%EXE_PATH%" serve --service-config "%CFG_PATH%" >nul
if errorlevel 1 (
    echo [eucli] 注册服务失败，可能需要管理员权限。
    exit /b 1
)

"%NSSM_EXE%" set "%EUCLI_SERVICE_NAME%" AppDirectory "%EUCLI_DEPLOY_DIR%" >nul
"%NSSM_EXE%" set "%EUCLI_SERVICE_NAME%" AppStdout "%LOG_FILE%.out" >nul
"%NSSM_EXE%" set "%EUCLI_SERVICE_NAME%" AppStderr "%LOG_FILE%" >nul
"%NSSM_EXE%" set "%EUCLI_SERVICE_NAME%" AppRotateFiles 1 >nul
"%NSSM_EXE%" set "%EUCLI_SERVICE_NAME%" Start SERVICE_AUTO_START >nul
"%NSSM_EXE%" set "%EUCLI_SERVICE_NAME%" AppExit Default Restart >nul

echo [eucli] 已注册服务: %EUCLI_SERVICE_NAME%
echo [eucli] 启动命令: sc start %EUCLI_SERVICE_NAME%
exit /b 0

:uninstall
sc query "%EUCLI_SERVICE_NAME%" >nul 2>&1
if errorlevel 1 (
    echo [eucli] 服务不存在: %EUCLI_SERVICE_NAME%
    exit /b 0
)
"%NSSM_EXE%" stop "%EUCLI_SERVICE_NAME%" >nul 2>&1
"%NSSM_EXE%" remove "%EUCLI_SERVICE_NAME%" confirm >nul
if errorlevel 1 (
    echo [eucli] 移除服务失败，可能需要管理员权限。
    exit /b 1
)
echo [eucli] 已移除服务: %EUCLI_SERVICE_NAME%
exit /b 0
