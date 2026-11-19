@echo off
rem 判断服务端进程是否在运行，两个退出码就是全部结论：
rem   exit /b 0  在运行
rem   exit /b 1  没在运行
rem 调用方需先 call lib\load-env.bat，这里只用它的 EUCLI_SERVER_EXE。
rem
rem 两个坑决定了写法：
rem   1. tasklist 的默认表格把程序名截断到 25 个字符，eucli-deep-wiki-server.exe
rem      正好超过，按截断后的名字比会永远判成没在运行 —— 所以取 CSV 输出；
rem   2. tasklist 在「没有匹配项」时同样返回 0，所以只能逐行比名字，不能看退出码。

set "SERVER_RUNNING="
for /f "usebackq tokens=1 delims=," %%A in (`tasklist /FI "IMAGENAME eq %EUCLI_SERVER_EXE%" /NH /FO CSV 2^>nul`) do (
    if /I "%%~A"=="%EUCLI_SERVER_EXE%" set "SERVER_RUNNING=1"
)

if not defined SERVER_RUNNING exit /b 1
exit /b 0
