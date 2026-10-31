<div align="center">

# ⚡️ pvfine

**现代化、高性能的某横版动作游戏 `Script.pvf` 跨平台桌面编辑器与解析套件**

[![Platform](https://img.shields.io/badge/Platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey?style=flat-square)](https://github.com)
[![PVF Versions](https://img.shields.io/badge/PVF%20Versions-86%20%7C%2090%20%7C%20100%20%7C%20110%20%7C%20115%20%7C%20TW-success?style=flat-square)](#-支持的-pvf-版本)
[![License](https://img.shields.io/badge/License-GPL--3.0-blue.svg?style=flat-square)](LICENSE)

<p align="center">
  <a href="#-支持的-pvf-版本">支持版本</a> •
  <a href="#-核心特性">核心特性</a> •
  <a href="#-快捷键操作">快捷键</a> •
  <a href="#-系统架构">系统架构</a> •
  <a href="#-go-sdk-独立调用">Go SDK</a> •
  <a href="#-开发与构建">开发与构建</a> •
  <a href="#-相关文档">相关文档</a>
</p>

</div>

---

## 📖 项目简介

`pvfine` 是一款面向某横版动作游戏归档文件的现代化跨平台桌面编辑器与解析套件。

采用 **Go + Wails v3 + Vue 3 + CodeMirror 6** 技术栈从零构建，将纯 Go 高性能解析内核与现代 Web 交互界面深度融合。具备流式按需解密、百万索引秒开、双向无损反编译、动画与资产预览、结构化数据 GUI 编辑及本地版本控制等特性。未修改保存保证原文件 **100% 逐字节完全一致**。

---

## ✨ 核心特性

- 🚀 **百万索引秒开与低内存开销**：基于流式按需解密、zlib 分块（Chunk-based）机制与持久化索引缓存，百万条目秒级载入；内置基于 SQLite 磁盘反向索引的高级字符串搜索。
- 📝 **结构化反编译与专业级编辑**：无损反编译 5 字节 Token 流，支持层级感知缩进、渲染排版规则定制、110US 占位符就地改写、Vim 模式、分屏与主题自适应；兼容 60ACT 格式归档编辑与保存。
- **归档文本编码切换**：支持自动识别及 UTF-8、GBK、Big5、CP949、UTF-16LE 显示编码选择，并记忆归档编码偏好。
- 🏷️ **智能标注系统与 LST 跳转**：规则驱动关联装备/道具/技能 `.lst` 列表映射，内嵌交互标注 Tag，一键跨脚本跳转；原生支持 110US `indexhash` 计算与自动登记。
- 🛠️ **标注与渲染规则编辑**：可从编辑器上下文直接查看和调整标注、渲染规则，支持按文件与 Section 匹配并即时应用。
- 🖼️ **丰富的多媒体与资产预览**：内置 ANI 动作关键帧动画播放器与通用二进制 ANI 解码、装备高保真属性面板、纯 Go 高性能 NPK / IMG 解码器（支持 DXT1/3/5 纹理与行内缩略图预览）。
- 🗂️ **结构化数据 GUI 可视化编辑**：为商店（`.shp`）、基础掉率、全局掉落、任务（`.qst`）、强化/增幅、锻造及技能参数（`.skl`）提供专用图形化表单，与代码随时切换，支持原子校验与安全提交。
- 🗃️ **轻量本地版本控制（VCS）**：内置基于 SQLite 元数据与 SHA-256 CAS 对象存储的旁路版本库，支持工作区修改检测、快照对比与安全回退。
- 🔍 **归档对比与合并工作台**：在编辑区标签页中比较两个归档的新增、删除与修改文件，支持双栏实时编辑、段落合入、批量合并、草稿保护及差异导出；提供差异文件与段落导航、撤销重做及快捷保存。
- 🧩 **JavaScript 脚本工作区**：集成轻量 Goja 沙箱，支持通过 JS 脚本批量改写归档、列表及文件集，提供事务隔离与 Diff 差异预览。
- 🛡️ **安全增量打包与原子落盘**：Dirty-chunk 差异重打包，非 ASCII 字符串智能路由进入 `sTrW` 防乱码，未修改保存 **100% 逐字节一致**，支持原子覆盖与自动备份。
- 📦 **资产导出与 MOD 发布**：支持单文件/批量导出、全量流式解包，以及向 `110USextend` 格式的 MOD 导出与依赖补齐。

---

## ⌨️ 快捷键操作

以下快捷键在归档对比与合并工作台中生效。

| 快捷键 | 操作 |
| --- | --- |
| `Alt+Left` / `Alt+Right` | 上一个 / 下一个差异文件 |
| `Shift+F7` / `F7` | 上一个 / 下一个差异段落 |
| `Alt+Up` / `Alt+Down` | 上一个 / 下一个差异段落 |
| `Ctrl+S`（macOS：`Cmd+S`） | 保存当前对比编辑 |
| `Ctrl+Z`（macOS：`Cmd+Z`） | 撤销 |
| `Ctrl+Y` 或 `Ctrl+Shift+Z`（macOS：`Cmd+Y` 或 `Cmd+Shift+Z`） | 重做 |

---

## 🏗️ 系统架构

采用严格关注点分离的分层架构：

```
┌────────────────────────────────────────────────────────────────────────┐
│                      Frontend (Vue 3 + Naive UI)                       │
│  - ToolBar / Explorer（虚拟树懒加载）/ CodeEditor（CM6 / Vim / 分屏）   │
│  - Previews（ANI 动画 / 装备面板 / 纹理）/ FileGUI（商店/任务/掉落/技能） │
│  - ScriptWorkbench / VersionPanel / BatchModal / AdvancedSearch ...    │
│  - CompareWorkbench / SideBySideDiff（归档对比与实时合并）             │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Wails v3 IPC
┌───────────────────────────────────▼────────────────────────────────────┐
│                    Services Layer (pvfine/services)                    │
│  - ArchiveService / EditorService / ExportService / PreviewService     │
│  - AnnotationService / SearchSQLite / FileGUIService / ScriptService   │
│  - VersionService / BatchService / ImageService / AutosaveService ...  │
│  - ArchiveDiffService（归档对比、草稿保存与批量合并）                  │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Go API
┌───────────────────────────────────▼────────────────────────────────────┐
│                              Core Engines                              │
│  - internal/pvf: 90US/100CN/110US/115US/TW 容器、种子、Token 与字符串池 │
│  - internal/preview: ANI 关键帧序列与图层调度模型                      │
│  - internal/script: Goja 沙箱与事务化宿主 API                          │
│  - internal/npk: NPK 只读解密、IMG 解析与 DXT 纹理解码                 │
│  - internal/annotations: 规则引擎、LST 双布局映射与联合索引            │
│  - internal/version: SQLite 元数据与 SHA-256 CAS 对象版本库            │
│  - internal/mod: MOD 模型与 110USextend 导出                           │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 💻 Go SDK 独立调用

`internal/pvf` 是一个完全解耦、不依赖 GUI 组件的纯 Go 核心包，可直接在命令行工具或脚本中独立使用：

```go
package main

import (
	"fmt"
	"log"

	"pvfine/internal/pvf"
)

func main() {
	// 1. 打开归档（自动识别 90US / 100CN / 110US / TW）
	archive, err := pvf.Open("Script.pvf")
	if err != nil {
		log.Fatalf("打开失败: %v", err)
	}
	defer archive.Close()

	// 2. 根据路径查找文件索引并反编译
	fileIndex, found := archive.Find("equipment/character/common/amulet/100300001.equ")
	if !found {
		log.Fatal("未找到指定文件")
	}
	text, _ := archive.Text(fileIndex)
	fmt.Println("脚本内容:\n", text)

	// 3. 修改内容并原子保存
	archive.SetText(fileIndex, text+"\n# pvfine customized\n")
	if err := archive.SaveAs("Script_edited.pvf"); err != nil {
		log.Fatalf("保存失败: %v", err)
	}
}
```

---

## 🛠️ 开发与构建

### 环境要求
- **Go**: `1.25.0` 或更高版本
- **Node.js**: `18.0.0` 或更高版本及 `npm`
- **Wails v3 CLI**: `go install github.com/wailsapp/wails/v3/cmd/wails3@latest`

### 开发与测试
```bash
# 启动热重载开发环境
wails3 task dev

# 运行 Go 内核与服务层测试
go test ./...

# 运行前端测试
cd frontend && npm test

# 真实 PVF 往返重打包回归测试（可选）
PVF_TESTFILE=/path/to/Script.pvf go test -v ./...
```

### 生产打包
```bash
wails3 task build      # 构建桌面应用程序
wails3 task package    # 打包平台发布安装包
```

---

## 📁 项目结构

```
.
├── cmd/                      # 辅助工具（如标注规则独立编辑器）
├── internal/                 # 核心内核（纯 Go 独立可测：PVF/ANI/NPK/脚本/规则/版本库）
├── services/                 # 应用服务层（桥接 Go 内核与前端 Wails IPC 状态）
├── frontend/                 # 前端工程（Vue 3 + TypeScript + Naive UI + Pinia）
├── docs/                     # 架构与格式规格设计文档
├── scripts/                  # 基准测试与工程脚本
└── Taskfile.yml              # 跨平台构建与开发任务编排
```

---

## 📚 相关文档

- [S4A21 PVF 格式逆向分析规格与数学算法](docs/FORMAT.md)
- [110US (Paged110) 容器结构与密钥分析](docs/PVF新格式分析.md)
- [PVF 变体架构与 100CN / TW 接入设计](docs/PVF变体架构.md)
- [115CN 内置支持与 GUI 兼容性检查](docs/115CN兼容性.md)
- [MOD 格式注册与 110USextend 导出约定](docs/MOD.md)
- [JavaScript 脚本工作区设计与 API](docs/脚本工作区设计.md)
- [标注与渲染规则设计说明](docs/rule-editor.md)

---

## 📋 免责声明

1. 本项目（`pvfine`）旨在用于**文件格式研究、逆向工程学习、单机交流与算法探索**。
2. 本项目不提供、不分发任何受版权保护的游戏客户端原始资源包。
3. 使用本项目对任何游戏资源进行的分析、提取、修改等操作，所有法律风险与责任均由使用者自行承担，作者及贡献者概不负责。
4. 请勿将本项目用于任何破坏计算机信息系统、侵犯知识产权或商业盈利行为。

---

## 📄 开源协议

本项目基于 [GNU General Public License v3.0 (GPL-3.0)](LICENSE) 协议开源。
