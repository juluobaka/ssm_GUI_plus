## 发布版说明（Release）

此目录用于放置“下载即用”的编译产物，方便直接打包上传到 GitHub Releases。

### windows-full（推荐）
- 适用：Windows 10/11
- 包含：`ssm.exe` + `scrcpy-server-v3.3.1` + 所有运行所需 DLL + 默认 `config.json` + Bestdori 缓存 `all.*.json`
- 用法：解压后直接运行 `ssm.exe`，浏览器打开提示的 `http://127.0.0.1:8765`

### windows-exe-only
- 适用：已自行准备好依赖 DLL 的用户
- 包含：仅 `ssm.exe`
- 注意：缺少 DLL 时会无法启动或投屏/解码不可用

