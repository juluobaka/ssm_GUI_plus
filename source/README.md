## 源码说明（Source）

此仓库根目录就是源码工程（Go + 前端 Vite）。这里的 `source/` 目录仅作为“源码入口提示”，方便和 `release/` 区分。

### 构建（Windows）
1. 前端构建：
   - `cd gui/frontend`
   - `npm install`
   - `npm run build`
2. 后端构建（仓库根目录）：
   - `go build .`

生成的可执行文件默认是 `ssm.exe`。

