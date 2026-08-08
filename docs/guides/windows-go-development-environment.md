# Windows Go 开发环境配置指导

本文记录 Windows 上 Go 工具链、CGO、C 编译器和 Go 编辑器工具的完整配置流程，目标是能够运行普通测试以及 go test -race ./...。

## 1. 配置目标

本次采用用户级安装，不要求管理员权限：

| 项目 | 路径或值 |
| --- | --- |
| Go | %LOCALAPPDATA%\Programs\Go |
| Go 版本 | go1.26.5 windows/amd64 |
| GOROOT | %LOCALAPPDATA%\Programs\Go |
| GOPATH | %USERPROFILE%\go |
| GOBIN | %USERPROFILE%\go\bin |
| WinLibs/MinGW-w64 | %LOCALAPPDATA%\Programs\WinLibs\mingw64\bin |
| C 编译器 | gcc |
| C++ 编译器 | g++ |
| 语言服务器 | gopls |

如果机器已有可用的 Go 或 C 编译器，应先检查并复用现有安装，避免重复安装。

## 2. 检查现有环境

在 PowerShell 中执行：

~~~powershell
Get-Command go, gcc, g++, clang, cl -ErrorAction SilentlyContinue

go version
go env GOROOT GOPATH GOBIN CGO_ENABLED CC CXX GOOS GOARCH
~~~

go test -race 在 Windows 上需要：

- CGO_ENABLED=1
- 可用的 C 编译器，通常是 MinGW-w64 GCC
- 与 Go 目标匹配的工具链；本配置使用 windows/amd64

## 3. 安装 Go

### 3.1 优先使用官方 Go 发布包

如果有管理员权限，也可以使用 winget：

~~~powershell
winget install --id GoLang.Go -e --source winget --accept-source-agreements --accept-package-agreements
~~~

如果 MSI 安装返回 Windows Installer 错误 1925，说明当前账户没有全机安装权限。此时使用官方 ZIP 做用户级安装。

以下示例使用 Go 1.26.5 Windows amd64 ZIP，并附带对应的 SHA-256 校验值：

~~~powershell
$goZip = Join-Path ([IO.Path]::GetTempPath()) 'go1.26.5.windows-amd64.zip'
$goExtract = Join-Path ([IO.Path]::GetTempPath()) 'go-extract'
$goRoot = Join-Path $env:LOCALAPPDATA 'Programs\Go'

Invoke-WebRequest -Uri 'https://go.dev/dl/go1.26.5.windows-amd64.zip' -OutFile $goZip -UseBasicParsing

$actual = (Get-FileHash $goZip -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne '97e6b2a833b6d89f9ff17d25419ac0a7e3b482a044e9ab18cdef834bd834fd38') {
  throw 'Go ZIP SHA-256 校验失败'
}

New-Item -ItemType Directory -Path $goExtract -Force | Out-Null
New-Item -ItemType Directory -Path (Split-Path $goRoot) -Force | Out-Null
tar.exe -xf $goZip -C $goExtract

if (-not (Test-Path (Join-Path $goExtract 'go\bin\go.exe'))) {
  throw 'Go 压缩包内容不完整或目录结构异常'
}

Move-Item (Join-Path $goExtract 'go') $goRoot
~~~

如果解压过程被中断，不要只保留部分目录；重新使用 tar.exe 完整解压，否则可能出现 package unicode is not in std 等标准库缺失错误。

## 4. 安装 MinGW-w64 C 编译器

本次使用 WinLibs 的 UCRT/POSIX 版本。它是包含 GCC、MinGW-w64、GDB 和常用构建工具的便携式发行包。

也可以先查看 WinLibs 的 winget 信息：

~~~powershell
winget show --id BrechtSanders.WinLibs.POSIX.UCRT -e --source winget
~~~

以下是本次验证过的版本和下载地址：

~~~powershell
$winlibsZip = Join-Path ([IO.Path]::GetTempPath()) 'winlibs-gcc.zip'
$winlibsRoot = Join-Path $env:LOCALAPPDATA 'Programs\WinLibs'

Invoke-WebRequest -Uri 'https://github.com/brechtsanders/winlibs_mingw/releases/download/16.1.0posix-14.0.0-ucrt-r4/winlibs-x86_64-posix-seh-gcc-16.1.0-mingw-w64ucrt-14.0.0-r4.zip' -OutFile $winlibsZip -UseBasicParsing

$actual = (Get-FileHash $winlibsZip -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne 'c406a22f8cac82559a3a1d96b62ff603f666499fb5ff4784e87b4eb6fa37dede') {
  throw 'WinLibs SHA-256 校验失败'
}

New-Item -ItemType Directory -Path $winlibsRoot -Force | Out-Null
tar.exe -xf $winlibsZip -C $winlibsRoot

$gcc = Join-Path $winlibsRoot 'mingw64\bin\gcc.exe'
if (-not (Test-Path $gcc)) {
  throw 'WinLibs 解压后未找到 gcc.exe'
}
~~~

发布包版本变化时，应从 WinLibs 发布页同步更新下载地址和 SHA-256，不要只替换其中一个。

## 5. 设置用户级环境变量

下面的脚本会保留原有用户 PATH，只在缺少时追加 Go、Go 工具和 GCC 路径：

~~~powershell
$goRoot = Join-Path $env:LOCALAPPDATA 'Programs\Go'
$goPath = Join-Path $env:USERPROFILE 'go'
$goBin = Join-Path $goPath 'bin'
$gccBin = Join-Path $env:LOCALAPPDATA 'Programs\WinLibs\mingw64\bin'

New-Item -ItemType Directory -Path $goPath -Force | Out-Null
New-Item -ItemType Directory -Path $goBin -Force | Out-Null

[Environment]::SetEnvironmentVariable('GOROOT', $goRoot, 'User')
[Environment]::SetEnvironmentVariable('GOPATH', $goPath, 'User')
[Environment]::SetEnvironmentVariable('GOBIN', $goBin, 'User')
[Environment]::SetEnvironmentVariable('CGO_ENABLED', '1', 'User')
[Environment]::SetEnvironmentVariable('CC', 'gcc', 'User')
[Environment]::SetEnvironmentVariable('CXX', 'g++', 'User')

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$parts = @($userPath -split ';' | Where-Object { $_ -and $_.Trim() })

foreach ($entry in @((Join-Path $goRoot 'bin'), $goBin, $gccBin)) {
  if (-not ($parts | Where-Object {
    $_.TrimEnd('\') -ieq $entry.TrimEnd('\')
  })) {
    $parts += $entry
  }
}

[Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
~~~

用户级环境变量只会自动进入新启动的进程。配置后应关闭并重新打开 PowerShell、Windows Terminal、VS Code 或其他 IDE。

## 6. 安装 gopls

在新终端中执行：

~~~powershell
go install golang.org/x/tools/gopls@latest
gopls version
~~~

gopls.exe 会安装到 GOBIN，编辑器可使用它提供代码补全、跳转、诊断和格式化支持。

## 7. 验证开发环境

~~~powershell
go version
gcc --version | Select-Object -First 1
g++ --version | Select-Object -First 1
go env GOROOT GOPATH GOBIN CGO_ENABLED CC CXX GOOS GOARCH
~~~

期望看到类似结果：

~~~text
go version go1.26.5 windows/amd64
CGO_ENABLED=1
CC=gcc
CXX=g++
GOOS=windows
GOARCH=amd64
~~~

在仓库根目录运行普通测试和竞态测试：

~~~powershell
go test ./...
go test -race ./...
~~~

配置完成后，预期两个命令均通过，且 go test -race ./... 能够使用 GCC/CGO 正常运行。

## 8. 常见问题

### CGO_ENABLED=0 或 C compiler cannot be found

检查当前终端是否为配置后新开的终端：

~~~powershell
Get-Command gcc
go env CGO_ENABLED CC CXX
~~~

如果 gcc 找不到，确认以下目录在用户 PATH 中：

~~~text
C:\Users\<用户名>\AppData\Local\Programs\WinLibs\mingw64\bin
~~~

也可以在当前 PowerShell 临时刷新：

~~~powershell
$env:CGO_ENABLED = '1'
$env:CC = 'gcc'
$env:CXX = 'g++'
$env:Path = "$env:LOCALAPPDATA\Programs\WinLibs\mingw64\bin;$env:Path"
~~~

### package unicode is not in std 或其他标准库缺失

这是 Go ZIP 解压不完整导致的，重新删除不完整的 Go 目录后，使用完整 ZIP 和 tar.exe 重新安装。不要仅复制 bin 目录。

### MSI 报错 1925

这是全机安装权限不足。使用官方 ZIP 安装到 %LOCALAPPDATA%\Programs\Go，并配置用户级环境变量即可，无需管理员权限。

### go test -race 仍不可用

确认以下三项同时成立：

~~~powershell
go env CGO_ENABLED  # 应为 1
go env CC           # 应为 gcc
gcc --version
~~~

另外，竞态检测只支持 Go 官方支持的目标平台；本配置使用的是 Windows amd64。

## 9. 示例版本与验证基线

以下版本和结果是本文的参考基线，不代表任何特定机器已经完成配置：

- Go 1.26.5
- WinLibs GCC 16.1.0 / MinGW-w64 UCRT POSIX
- gopls v0.23.0
- CGO_ENABLED=1
- go test ./... 通过
- go test -race ./... 通过
