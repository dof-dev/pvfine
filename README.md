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
- **轻量本地版本控制**：基于 SQLite 与 CAS 对象存储构建旁路版本库，支持工作区修改检测、快照提交对比与安全检出回退。
- **NPK 与贴图解析**：原生支持 NPK 资源包读取、DXT 纹理分块解码与编辑器内嵌预览。
- **智能标注与 LST 跳转**：规则驱动关联 LST 列表，代码内嵌标注 Tag，支持跨脚本一键导航跳转。
- **安全原子保存**：增量分块打包，未修改保存保证原文件 **100% 字节级一致**，写回全程采用临时文件加原子重命名（Atomic Rename）与自动备份。

---

## ✨ 核心特性

### 🚀 极速解析与低内存开销
- **秒级载入**：对超 100 万个条目的归档，解密头部并建立内存索引耗时 < 1 秒。
- **按需分块加载**：采用虚拟分块（Chunk-based）机制与 LRU 缓存，只在用户浏览/打开特定文件时解密对应数据块，无需将全量数据一次性载入内存。

### 🌲 智能资源管理器
- **树状目录懒加载**：分层动态展开，实时显示各目录下的子节点统计数量。
- **修改状态动态标记**：直观标记已修改文件（Dirty 状态）与变动目录，修改范围一目了然。
- **游标分页检索（Cursor Pagination）**：支持在百万级路径中进行全量不区分大小写的模糊搜索与精确匹配，滚动按需加载，输入防抖平滑无顿挫。
- **便捷文件操作**：支持单击/双击打开行为自定义、复制相对/绝对文件路径、快速定位并高亮当前编辑文件。

### 📝 结构化反编译与专业级编辑
- **脚本反编译（DataType 1）**：
  - 深度解析 5-byte Token 流，无损反编译为语义清晰的脚本代码。
  - **层级感知缩进**：自动根据代码段落（`[tag]` 至 `[/tag]`）计算嵌套缩进深度。
  - **专用排版格式化规则**：针对 `skill data up`（每行 7 个 token）、`skill levelup`（每行 3 个 token）、`.lst` 列表（每行 2 个 token）等特定配置应用整洁换行排版。
  - **完整语法支持**：支持行内串 `` `...` ``、块串标记 `{5=`...`}` 与 `{7=`...`}`，支持单行注释 `#`。
- **本地化文本编码修复（DataType 3）**：
  - 针对韩服转制特有的「EUC-KR 字节被逐字节按 CP437 字体映射进 UTF-16」的历史遗留乱码，提供内置自动识别与还原修复（还原为标准的 CP949 / EUC-KR 文本）。
- **专业级代码编辑体验**：
  - 基于 **CodeMirror 6** 深度定制，内置行号、活动行高亮、括号匹配与行内查找替换。
  - **分屏编辑**：支持水平（左右并排）与垂直（上下并排）多窗格分屏，自由拖拽拆分条调整面板尺寸。
  - **Vim 编辑模式**：可在设置中随时启用 Vim 键位绑定，键盘流操作丝滑流畅。
  - **深浅色主题自适应**：原生支持深色（Dark）、浅色（Light）及跟随系统自动切换。
  - **多标签页管理**：支持标签页批量关闭（关闭右侧/关闭其他/全部关闭），400ms 防抖同步至内存修改 Overlay。

### 🏷️ 智能标注系统与 LST 跨文件跳转
- **规则驱动关联引擎**：通过灵活的 JSON 标注规则，自动关联装备、道具、技能等 `.lst` 列表映射配置与引用关系。
- **内嵌交互标注 Tag**：在脚本关键 ID 旁直观展示对应名称，支持设置 Tag 摆放位置（跟随目标后、行尾固定或隐藏）。
- **一键跨文件跳转**：按住 <kbd>Cmd</kbd> / <kbd>Ctrl</kbd> 单击标注标签，直接在编辑器中定位并打开被引用的目标脚本文件。
- **独立标注编辑器**：内置 `cmd/annotation-editor` 工具，支持可视化调试与编辑标注规则。

### 🖼️ NPK 资源读取与 DXT 图像解析
- **高性能 NPK 解析内核**：纯 Go 实现的只读 NPK / IMG 解码器，秒级扫描并索引数万张贴图资源。
- **DXT 纹理解码**：原生支持 DXT1、DXT3、DXT5 压缩纹理解压，以及 1555、4444、8888 等常见像素格式。
- **行内贴图与悬停预览**：脚本中的贴图标注支持直接嵌入 16x16 行内缩略图；鼠标悬停标签时弹出清晰大图预览卡片。

### 🗃️ 资源版本控制（VCS）
- **本地旁路版本库**：在 PVF 归档旁自动创建 `.pvfine` 侧边版本库，基于 SQLite 元数据与 SHA-256 CAS 不可变对象存储。
- **版本历史与提交**：直观的版本控制面板，支持查看工作区改动清单、填写提交说明并生成版本快照（支持快捷键提交）。
- **快照比对与安全检出**：支持对比历史快照改动差异，一键回滚未提交改动或检出历史版本，不破坏原 PVF 文件结构。

### 📑 嵌套书签簿与文件集持久化
- **嵌套书签簿**：支持创建多级树状嵌套书签簿，自由归类、编辑与重命名常用脚本，点击即刻直达。
- **文件集持久化（FileSets）**：支持将跨目录的相关文件编组收藏，配置自动持久化保存，支持一键批量导出选中文件集。
- **最近打开记录**：自动追踪最近编辑与访问的历史文件，快速重新载入。

### ⚡ 脚本批处理与外部资源导入
- **Token 级批处理引擎**：支持针对 5-byte Token 树和 Section 节点的批量修改规则执行，提供变更数量统计与可视化的 Diff 差异对比预览，确认无误后再安全应用。
- **外部资源一键导入**：支持从本地文件系统批量导入文件与目录至归档指定目录，智能识别脚本/原始二进制，提供冲突与覆盖预览。

### 🛡️ 增量打包、原子落盘与退出防护
- **Dirty-chunk 差异重打包**：
  - 保存时自动比对修改分块，仅对产生变动的 Chunk 重新执行 zlib 压缩与 `"BodY"` 加密，未改动分块直接复用原始密文。
  - 自动维护并追加更新 `sTrA`（UTF-8）与 `sTrW`（UTF-16LE）双字符串池及客户端检索二分 `HashTable`。
- **字节级一致性验证**：未修改的归档经重打包后，与原始文件保持 100% 逐字节完全一致（已通过真实 100MB 归档交叉单测）。
- **保存确认与自动备份**：覆盖保存源文件时弹出确认弹窗，并支持自动将原文件备份为 `.bak`。
- **原子保存与退出防丢**：数据先写入临时文件（`*.pvftmp`）再执行 `os.Rename` 原子覆盖；窗口关闭时提供未保存修改拦截防护（CloseGuard），杜绝数据丢失。

### 📦 提取与整包解包
- **单文件与批量导出**：支持一键导出当前文件或按选定范围批量导出原始二进制文件。
- **流式全量解包**：支持在后台协程将整包数万/数百万文件并发解压导出至指定目录，前端状态栏实时进度条显示，支持随时优雅取消。

---

## ⌨️ 快捷键操作

| 快捷键 (macOS) | 快捷键 (Win / Linux) | 功能说明 |
|:---|:---|:---|
| <kbd>Cmd</kbd> + <kbd>O</kbd> | <kbd>Ctrl</kbd> + <kbd>O</kbd> | 打开 PVF 归档文件 |
| <kbd>Cmd</kbd> + <kbd>S</kbd> | <kbd>Ctrl</kbd> + <kbd>S</kbd> | 保存全部修改至原归档（附备份确认） |
| <kbd>Cmd</kbd> + <kbd>Shift</kbd> + <kbd>S</kbd> | <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>S</kbd> | 另存为新 PVF 文件 |
| <kbd>Cmd</kbd> + <kbd>W</kbd> | <kbd>Ctrl</kbd> + <kbd>W</kbd> | 关闭当前编辑器标签页 |
| <kbd>Cmd</kbd> + <kbd>\</kbd> | <kbd>Ctrl</kbd> + <kbd>\</kbd> | 左右拆分编辑器（分栏分屏） |
| <kbd>Cmd</kbd> + <kbd>Shift</kbd> + <kbd>\</kbd> | <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>\</kbd> | 上下拆分编辑器（多行分屏） |
| <kbd>Cmd</kbd> + <kbd>Enter</kbd> | <kbd>Ctrl</kbd> + <kbd>Enter</kbd> | 快速提交当前版本（Version Commit） |
| <kbd>Cmd</kbd> + <kbd>Click</kbd> | <kbd>Ctrl</kbd> + <kbd>Click</kbd> | 单击标注标签快速跳转引用脚本 |
| <kbd>Cmd</kbd> + <kbd>F</kbd> | <kbd>Ctrl</kbd> + <kbd>F</kbd> | 编辑器内查找与替换 |

---

## 🏗️ 系统架构

本项目采用严格关注点分离的模块化架构：

```
┌────────────────────────────────────────────────────────────────────────┐
│                      Frontend (Vue 3 + Naive UI)                       │
│  - ToolBar: 归档生命周期 / 导入 / 解包 / 搜索 / 版本 / 侧栏切换         │
│  - Explorer: 虚拟树形懒加载 / 修改标记 / 路径复制 / 游标分页搜索       │
│  - CodeEditor: CodeMirror 6 / Vim 模式 / LST 标注与贴图 / 分屏协同     │
│  - Sidebars & Modals: 嵌套书签簿 / 文件集 / 版本面板 / 批处理 / 设置   │
│  - Pinia Stores: archive / editor / bookmarks / fileSets / version ... │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Wails v3 IPC (Auto Bindings)
┌───────────────────────────────────▼────────────────────────────────────┐
│                    Services Layer (pvfine/services)                    │
│  - ArchiveService: 归档加载、生命周期、目录树与检索、资源导入与解包    │
│  - EditorService: 文本反编译、Overlay 缓存、落盘原子保存、导出与备份   │
│  - VersionService: 本地版本库生命周期、提交、快照差异与检出            │
│  - AnnotationService: 规则引擎绑定、LST 索引构建与关联计算             │
│  - ImageService: NPK 资源索引、DXT 图像解码与缩略图缓存                │
│  - BatchService: 批处理规则变换、语法树扫描与 Diff 差异生成            │
│  - BookmarkService & FileSetService: 嵌套书签簿与文件集持久化          │
│  - SettingsService: 全局用户配置 (主题、打开方式、Vim、备份等)         │
│  - Core: 读写锁守卫的共享并发状态模型                                  │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Go API
┌───────────────────────────────────▼────────────────────────────────────┐
│                              Core Engines                              │
│  - internal/pvf: 头部/GRPI分块/Token双向反编译/字符串池/差异重打包     │
│  - internal/npk: NPK 容器读取、IMG 图像帧解析与 DXT1/3/5 解码          │
│  - internal/annotations: 规则加载、LST 映射、装备/道具联合索引         │
│  - internal/version: SQLite 版本库管理与 SHA-256 CAS 对象存储          │
└────────────────────────────────────────────────────────────────────────┘
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

### 运行测试与基准测试

运行核心内核与服务层单元测试（涵盖 PVF、NPK、标注引擎、版本控制与应用服务）：

```bash
go test ./...
```

使用真实 PVF 归档进行完整往返重打包与字节级一致性回归验证：

```bash
PVF_TESTFILE=/path/to/Script.pvf go test -v ./...
```

运行真实资源（PVF 与 NPK）加载与索引基准测试：

```bash
PVF_TESTFILE=/path/to/Script.pvf NPK_TESTDIR=/path/to/ImagePacks2 ./scripts/benchmark.sh
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
├── cmd/
│   └── annotation-editor/    # 独立标注规则可视化编辑与调试服务
├── internal/                 # 核心内核模块（纯 Go 独立可测）
│   ├── pvf/                  # PVF 解析与打包引擎（加解密、反编译、分块、Token、字符串池、批处理）
│   ├── npk/                  # NPK 资源包读取、IMG 图像帧解析与 DXT1/3/5 解码器
│   ├── annotations/          # 标注规则引擎、LST 列表关联与装备/道具联合索引
│   └── version/              # 本地版本库模型、SQLite 元数据与 SHA-256 CAS 对象存储
├── services/                 # 应用服务层：桥接 Go 内核与前端 IPC 状态
│   ├── core.go               # 线程安全共享 Core、目录树索引与搜索状态
│   ├── archive.go            # ArchiveService：归档打开、懒加载目录树、游标搜索
│   ├── editor.go             # EditorService：文本反编译、内存编辑、保存与解包
│   ├── annotations.go        # AnnotationService：标注查询与 LST 关联
│   ├── batch.go              # BatchService：批处理规则解析与 Diff 预览
│   ├── bookmarks.go          # BookmarkService：嵌套书签簿增删改查
│   ├── filesets.go           # FileSetService：文件集持久化管理
│   ├── image_service.go      # ImageService：NPK 资源管理与图像缓存
│   ├── import.go             # 资源批量导入与冲突预览
│   ├── settings.go           # SettingsService：全局用户配置与偏好设置
│   └── version.go            # VersionService：版本仓库控制与快照管理
├── frontend/                 # 前端工程：Vue 3 + TypeScript 响应式桌面 UI
│   ├── src/
│   │   ├── components/       # UI 组件 (ToolBar, Explorer, EditorTabs, EditorPane, CodeEditor,
│   │   │                     #         BookmarkSidebar, FileSetSidebar, VersionPanel, BatchProcessModal,
│   │   │                     #         AdvancedSearchModal, ImportModal, SettingsModal, StatusBar, CloseGuard)
│   │   ├── stores/           # Pinia 状态管理 (archive, explorer, editor, bookmarks, fileSets,
│   │   │                     #                 version, images, settings, batch, advancedSearch, import)
│   │   ├── theme.ts          # 深色 / 浅色 / 跟随系统主题配色体系
│   │   ├── App.vue           # 主界面布局、侧边栏集成与全局快捷键监听
│   │   └── main.ts           # 前端入口
│   └── bindings/             # Wails 自动生成的 TypeScript 服务端点绑定
├── docs/                     # 技术规格文档
│   └── FORMAT.md             # S4A21 PVF 二进制格式逆向分析规格与数学算法
├── scripts/                  # 工程脚本（基准测试、版本更新注入等）
│   ├── benchmark.sh          # 真实资源性能基准测试脚本
│   └── set-build-version.js  # 跨平台构建版本注入
└── Taskfile.yml              # 跨平台构建与开发任务编排
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
