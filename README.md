<div align="center">

# ⚡️ pvfine

**现代化、高性能的某横版动作游戏 `Script.pvf` 跨平台桌面编辑器与解析套件**

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Wails v3](https://img.shields.io/badge/Wails-v3.0.0--beta.12-DF0000?style=flat-square)](https://v3.wails.io)
[![Vue 3](https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.x-3178C6?style=flat-square&logo=typescript)](https://www.typescriptlang.org/)
[![Platform](https://img.shields.io/badge/Platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey?style=flat-square)](https://github.com)
[![License](https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square)](LICENSE)

<p align="center">
  <a href="#-核心特性">核心特性</a> •
  <a href="#-快捷键操作">快捷键</a> •
  <a href="#-系统架构">系统架构</a> •
  <a href="#-go-sdk-独立调用">Go SDK</a> •
  <a href="#-开发与构建">开发与构建</a> •
  <a href="#-项目结构">项目结构</a> •
  <a href="#-免责声明">免责声明</a>
</p>

</div>

---

## 📖 项目简介

`pvfine` 是一款面向某横版动作游戏归档文件的现代化桌面编辑器与解析套件。

采用 **Go + Wails v3 + Vue 3 + CodeMirror 6** 技术栈从零构建，将纯 Go 高性能解析内核与现代 Web 交互界面结合：
- **百万索引秒开**：基于流式按需解密与 zlib 分块解压，低内存占用。
- **双向无损反编译**：将 5 字节二进制 Token 流还原为可读性高、具备语法层级感知的结构化脚本。
- **安全原子保存**：增量分块打包，未修改保存保证原文件 **100% 字节级一致**，写回全程采用临时文件加原子重命名（Atomic Rename），避免文件损坏。

---

## ✨ 核心特性

### 🚀 极速解析与低内存开销
- **秒级载入**：对超 100 万个条目的归档，解密头部并建立内存索引耗时 < 1 秒。
- **按需分块加载**：采用虚拟分块（Chunk-based）机制与 LRU 缓存，只在用户浏览/打开特定文件时解密对应数据块，无需将全量数据一次性载入内存。

### 🌲 智能资源管理器
- **树状目录懒加载**：分层动态展开，实时显示各目录下的子节点统计数量。
- **游标分页检索（Cursor Pagination）**：支持在百万级路径中进行全量不区分大小写的模糊搜索，滚动按需加载，输入防抖平滑无顿挫。

### 📝 结构化反编译与双向编辑
- **脚本反编译（DataType 1）**：
  - 深度解析 5-byte Token 流，无损反编译为语义清晰的脚本代码。
  - **层级感知缩进**：自动根据代码段落（`[tag]` 至 `[/tag]`）计算嵌套缩进深度。
  - **专用排版格式化规则**：针对 `skill data up`（每行 7 个 token）、`skill levelup`（每行 3 个 token）、`.lst` 列表（每行 2 个 token）等特定配置应用整洁换行排版。
  - **完整语法支持**：支持行内串 `` `...` ``、块串标记 `{5=`...`}` 与 `{7=`...`}`，支持单行注释 `#`。
- **本地化文本编码修复（DataType 3）**：
  - 针对韩服转制特有的「EUC-KR 字节被逐字节按 CP437 字体映射进 UTF-16」的历史遗留乱码，提供内置自动识别与还原修复（还原为标准的 CP949 / EUC-KR 文本）。
- **专业级代码编辑体验**：
  - 基于 **CodeMirror 6** 深度定制，搭配暗黑主题、行号、活动行高亮、括号匹配与行内搜索。
  - 多标签页管理，400ms 防抖同步至内存修改 Overlay，实时显示未保存脏标记（Dirty Dots）。

### 🛡️ 增量打包与原子安全落盘
- **Dirty-chunk 差异重打包**：
  - 保存时自动比对修改分块，仅对产生变动的 Chunk 重新执行 zlib 压缩与 `"BodY"` 加密，未改动分块直接复用原始密文。
  - 自动维护并追加更新 `sTrA`（UTF-8）与 `sTrW`（UTF-16LE）双字符串池及客户端检索二分 `HashTable`。
- **字节级一致性验证**：未修改的归档经重打包后，与原始文件保持 100% 逐字节完全一致（已通过真实 100MB 归档交叉单测）。
- **原子保存机制**：数据先写入临时文件（`*.pvftmp`），落盘成功后执行 `os.Rename` 原子覆盖，杜绝意外崩溃破坏原归档。

### 📦 提取与整包解包
- **单文件导出**：支持一键将当前打开的文件以原始二进制格式导出到磁盘。
- **流式全量解包**：支持在后台协程将整包数万/数百万文件并发解压导出至指定目录，前端状态栏实时进度条显示，支持随时优雅取消。

---

## ⌨️ 快捷键操作

| 快捷键 (macOS) | 快捷键 (Win / Linux) | 功能说明 |
|:---|:---|:---|
| <kbd>Cmd</kbd> + <kbd>O</kbd> | <kbd>Ctrl</kbd> + <kbd>O</kbd> | 打开 PVF 归档文件 |
| <kbd>Cmd</kbd> + <kbd>S</kbd> | <kbd>Ctrl</kbd> + <kbd>S</kbd> | 保存全部修改至原归档（原子写） |
| <kbd>Cmd</kbd> + <kbd>Shift</kbd> + <kbd>S</kbd> | <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>S</kbd> | 另存为新 PVF 文件 |
| <kbd>Cmd</kbd> + <kbd>W</kbd> | <kbd>Ctrl</kbd> + <kbd>W</kbd> | 关闭当前编辑器标签页 |

---

## 🏗️ 系统架构

本项目采用严格关注点分离的模块化架构：

```
┌─────────────────────────────────────────────────────────┐
│              Frontend (Vue 3 + Naive UI)                │
│  - ToolBar: 文件操作 / 导出 / 解包调度                  │
│  - Explorer: 虚拟树形目录懒加载 / 游标分页搜索           │
│  - CodeEditor: CodeMirror 6 编辑器 / 多标签协同         │
│  - Pinia Stores: archive / explorer / editor 响应式状态 │
└────────────────────────────┬────────────────────────────┘
                             │ Wails v3 IPC (Auto Bindings)
┌────────────────────────────▼────────────────────────────┐
│              Services Layer (pvfine/services)           │
│  - ArchiveService: 归档加载、生命周期、树与搜索索引服务 │
│  - EditorService: 文本反编译视图、Overlay 缓存、落盘操作 │
│  - Core: 读写锁守卫的共享并发状态模型                   │
└────────────────────────────┬────────────────────────────┘
                             │ Go API
┌────────────────────────────▼────────────────────────────┐
│            Core Engine (pvfine/internal/pvf)            │
│  - crypt: LCG 流加解密 (HeaD / HASH / GRPI / BodY 等)    │
│  - archive: 头部解析、分块管理、按需解密解压            │
│  - file & script: 5-byte Token 流双向反编译与重构       │
│  - stringpool: sTrA / sTrW 双字符串池解析与增量维护     │
│  - save: 差异重打包、Hash 表二分索引生成与原子覆写      │
└─────────────────────────────────────────────────────────┘
```

> 详细的 PVF 文件布局与解密算法数学推导，请参阅 [docs/FORMAT.md](docs/FORMAT.md)。

---

## 💻 Go SDK 独立调用

`internal/pvf` 是一个完全解耦、不依赖任何 GUI 组件的纯 Go 核心包，可以直接在命令行工具或自动化脚本中引入：

```go
package main

import (
	"fmt"
	"log"

	"pvfine/internal/pvf"
)

func main() {
	// 1. 打开归档
	archive, err := pvf.Open("Script.pvf")
	if err != nil {
		log.Fatalf("打开失败: %v", err)
	}

	fmt.Printf("归档已加载: 共 %d 个文件，%d 个数据块\n", 
		archive.FileCount(), archive.GroupCount())

	// 2. 根据路径查找文件索引
	fileIndex, found := archive.Find("equipment/character/common/amulet/100300001.equ")
	if !found {
		log.Fatal("未找到指定文件")
	}

	// 3. 读取并反编译为可读文本
	text, err := archive.Text(fileIndex)
	if err != nil {
		log.Fatalf("反编译失败: %v", err)
	}
	fmt.Println("当前脚本内容:\n", text)

	// 4. 修改内容并写入内存 Overlay 缓冲区
	newText := text + "\n# pvfine customized\n"
	if err := archive.SetText(fileIndex, newText); err != nil {
		log.Fatalf("设置修改失败: %v", err)
	}

	// 5. 新增文件到归档
	archive.AddFileText("etc/custom_config.txt", "[name]\n\t`test`\n", pvf.TypeScript)

	// 6. 原子另存为新归档文件
	if err := archive.SaveAs("Script_edited.pvf"); err != nil {
		log.Fatalf("另存为失败: %v", err)
	}
	fmt.Println("新归档保存成功！")
}
```

---

## 🛠️ 开发与构建

### 前置要求

- **Go**: `1.25.0` 或更高版本
- **Node.js**: `18.0.0` 或更高版本，以及 `npm`
- **Wails v3 CLI**:
  ```bash
  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
  ```

### 开发环境

启动前后端热重载开发服务器：

```bash
wails3 task dev
```

如需仅对前端进行调试：

```bash
cd frontend
npm install
npm run dev
```

### 运行测试

运行核心内核与服务层单元测试：

```bash
go test ./...
```

使用真实 PVF 归档进行完整往返重打包与字节级一致性回归验证：

```bash
PVF_TESTFILE=/path/to/Script.pvf go test -v ./...
```

### 生产打包

构建完整桌面可执行程序（自动执行前端构建与二进制嵌入）：

```bash
# 构建桌面应用程序
wails3 task build

# 或打包对应平台的发布包
wails3 task package
```

编译产物将生成在 `bin/` 目录中。

---

## 📁 项目结构

```
.
├── main.go               # Wails v3 入口：原生窗口配置、生命周期与服务注册
├── internal/pvf/         # 核心内核：纯 Go 独立可测的 PVF 解析与打包引擎
│   ├── crypt.go          # LCG 伪随机流加解密算法（对称 XOR）
│   ├── archive.go        # 归档数据模型、头部解密、GRPI 分块管理与内存索引
│   ├── file.go           # 格式化规则、层级嵌套缩进计算与脚本反编译
│   ├── script.go         # 脚本分词器与 Token 流编译器
│   ├── stringpool.go     # sTrA (UTF-8) 与 sTrW (UTF-16LE) 字符串池维护
│   ├── hashtable.go      # 客户端快速检索二分查找表生成
│   ├── save.go           # 增量打包、原子写入与流式整包解压
│   ├── archive_test.go   # 归档解析与回写测试
│   └── script_test.go    # 脚本编解码与缩进回归测试
├── services/             # 应用服务层：桥接 Go 内核与前端 IPC 状态
│   ├── core.go           # 线程安全共享 Core、目录树索引与搜索状态
│   ├── archive.go        # ArchiveService：归档打开、懒加载目录树、游标搜索
│   ├── editor.go         # EditorService：文本反编译、内存编辑、保存与解包
│   └── services_test.go  # 服务层功能集成测试
├── frontend/             # 前端工程：Vue 3 + TypeScript 响应式桌面 UI
│   ├── src/
│   │   ├── components/   # UI 组件 (ToolBar, Explorer, EditorTabs, CodeEditor, StatusBar)
│   │   ├── stores/       # Pinia 状态管理 (archive, explorer, editor)
│   │   ├── App.vue       # 主界面布局与全局快捷键监听
│   │   └── main.ts       # 前端入口
│   └── bindings/         # Wails 自动生成的 TypeScript 服务端点绑定
├── docs/                 # 技术文档
│   └── FORMAT.md         # S4A21 PVF 二进制格式逆向分析规格与数学算法
└── Taskfile.yml          # 跨平台构建与开发任务编排
```

---

## 📋 免责声明

1. 本项目（`pvfine`）旨在用于**文件格式研究、逆向工程学习、单机交流与算法探索**。
2. 本项目不提供、不分发任何受版权保护的游戏客户端原始资源包。
3. 使用本项目对任何游戏资源进行的分析、提取、修改等操作，所有法律风险与责任均由使用者自行承担，作者及贡献者概不负责。
4. 请勿将本项目用于任何破坏计算机信息系统、侵犯知识产权或商业盈利行为。

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 协议开源。
