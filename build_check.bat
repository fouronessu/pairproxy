@echo off
cd /d D:\pairproxy
go build .\cmd\sproxy\ 1>D:\build_out.txt 2>D:\build_err.txt
echo %errorlevel% >D:\build_ec.txt
if exist sproxy.exe (echo yes>D:\build_exe.txt) else (echo no>D:\build_exe.txt)
