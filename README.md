# pvfine

基于 **wails3 + Vue3 + naive-ui + CodeMirror 6** 的桌面版 PVF 归档编辑器。
用于打开、浏览、在内存中编辑并重新打包 DNF 脚本资源包(`Script.pvf`,S4A21 加密变体)。

## 功能

- **打开归档**:菜单/拖拽/`Cmd+O` 加载 PVF,自动解密头部、构建目录索引(100 万级文件 <1s)
- **资源管理器**:懒加载目录树(带子节点计数)+ 路径搜索(游标分页,支持滚动加载更多)
- **编辑器**:多标签页,脚本分派为反编译文本可编辑,UTF-16 本地化文本自动修复韩服 CP437 乱码;
  CodeMirror 6 编辑,脏标记圆点,防抖 400ms 同步到内存 overlay
- **保存**:写回源文件(临时文件 + rename 原子写,失败不破坏原文件)/ 另存为新 PVF
- **导出/解包**:单文件导出原始字节;整包解包带进度回调与取消
- **状态栏**:归档路径、文件数/块数、已修改数、当前文件、解包进度
- 快捷键:`Cmd+O` 打开、`Cmd+S` 保存、`Cmd+Shift+S` 另存为、`Cmd+W` 关闭标签

## 技术栈

- `internal/pvf`:纯 Go 解析打包内核(LCG 流加解密、zlib 分块、字符串池、token 编解码),单测覆盖
- `services`:Wails3 服务层(Archive/Editor),共享 core 并发安全
- `frontend/binbindings`:一键生成的 TS bindings(`wails3 generate bindings`)
- 解析内核与打包逻辑对真实 100MB Script.pvf 做过交叉验证,未修改保存字节级一致

## 开发

```bash
wails3 task dev       # 开发模式(热重载) — 本地 services 单测:
PVF_TESTFILE=/path/to/Script.pvf go test ./...
```

## 构建

```bash
# 前端(生成 frontend/dist)
cd frontend && npm run build
# 主程序(嵌入 dist)
wails3 task build     # 或 go build ./...
```

## 目录结构

```
main.go            # Wails3 入口:窗口/服务注册
internal/pvf/      # 解析打包内核(独立可测)
services/          # ArchiveService / EditorService + 共享 core
frontend/src/
  stores/          # pinia:archive / explorer / editor
  components/      # ToolBar / Explorer / EditorTabs / CodeEditor / StatusBar
bindings/          # wails3 生成的 TS 绑定
