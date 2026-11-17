Option Explicit

' 只做一件事：把 start.bat 以隐藏窗口的方式拉起来。
' 启动逻辑全部留在 start.bat 里，这里不重复任何判断。

Dim shell, fso, baseDir, batPath
Set shell = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")

baseDir = fso.GetParentFolderName(WScript.ScriptFullName)
batPath = baseDir & "\start.bat"

If Not fso.FileExists(batPath) Then
    WScript.Quit 1
End If

' 0 = 隐藏窗口，False = 不等待
shell.Run """" & batPath & """", 0, False
