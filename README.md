<div align="center">

# ⚡️ pvfine

**现代化、高性能的某横版动作游戏 `Script.pvf` 跨平台桌面编辑器与解析套件**

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Wails v3](https://img.shields.io/badge/Wails-v3.0.0--beta.12-DF0000?style=flat-square)](https://v3.wails.io)
[![Vue 3](https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.x-3178C6?style=flat-square&logo=typescript)](https://www.typescriptlang.org/)
[![Platform](https://img.shields.io/badge/Platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey?style=flat-square)](https://github.com)
[![License](https://img.shields.io/badge/License-GPL--3.0-blue.svg?style=flat-square)](LICENSE)

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
- **百万索引秒开**：基于流式按需解密与 zlib 分块解压，低内存占用；内置持久化索引缓存，二次载入零等待。
- **多版本归档与变体兼容**：全面兼容标准 90US、90CN 非标准种子变体（种子恢复与 HASH 段保护）及新一代 **110US（Paged110）分页归档**（AES-256-CBC 页首保护、`sk.dat` 密钥链解密与无损流式写回）。
- **双向无损反编译**：将 5 字节二进制 Token 流还原为可读性高、具备语法层级感知的结构化脚本。
- **110US 字符串表与占位符就地编辑**：深度解析 `<表号::键名>` 占位符、字符串表与伴生 `list/*_indexhash.etc`，支持在编辑器内直接修改译文与自动生成 `hashindex`。
- **丰富的多媒体与资产预览**：原生集成 ANI 动作动画播放器、装备属性面板与 NPK / DXT 纹理渲染，按文件类型自动分派预览宿主。
- **结构化数据 GUI 编辑**：为 `.shp` 商店、基础掉率与全局掉落提供专用可视化编辑器，按文件类型注册 GUI provider 并与文本编辑随时切换；编辑提交内存改动，原归档不自动写盘。
- **轻量本地版本控制**：基于 SQLite 与 CAS 对象存储构建旁路版本库，支持工作区修改检测、快照提交对比与安全检出回退。
- **智能标注与 LST 跳转**：规则驱动关联 LST 列表（兼顾 90US 目录相对与 110US 根相对双布局），代码内嵌标注 Tag，支持跨脚本一键导航跳转。
- **JavaScript 脚本工作区**：内置 Goja 沙箱按脚本批量改写归档，支持 LST 列表、文件集与文件增删改 API，事务隔离、预览确认后选择性应用。
- **安全原子保存**：增量分块打包与规范字符串池路由（非 ASCII 自动写入 `sTrW` 杜绝乱码），未修改保存保证原文件 **100% 字节级一致**，写回全程采用临时文件加原子重命名（Atomic Rename）与自动备份。

---

## ✨ 核心特性

### 🚀 极速解析与低内存开销
- **秒级载入**：对超 100 万个条目的归档，解密头部并建立内存索引耗时 < 1 秒。
- **按需分块加载**：采用虚拟分块（Chunk-based）机制与 LRU 缓存，只在用户浏览/打开特定文件时解密对应数据块，无需将全量数据一次性载入内存。
- **持久化索引缓存**：引入轻量级搜索与元数据索引缓存机制，基于归档特征与修改时间快速比对，二次打开省去重复扫描列表与建立索引开销，实现即开即用。
- **SQLite 高级字符串搜索**：基于磁盘反向索引与游标分页查询，支持大型归档按需构建、查询结果缓存、进度反馈与取消操作；归档编辑后自动使旧结果失效。

### 🌐 多版本归档与变体兼容（Paged110 / 90CN）
- **110US（Paged110）分页容器与无损写回**：
  - 完整支持新一代 110US 分页归档结构，按 10 MiB 页粒度解密前 10,240 字节的 AES-256-CBC 保护块；
  - 自动解密同目录 `sk.dat`（RSA-1024 PKCS#1 v1.5 私钥）与 metadata key 密钥链，推导出 52 组 32 字节页密钥；
  - 采用独创的 HASH 结构反解算法推导段种子，支持对分页归档增删改文件、重建 HASH 段与重编名称池；写回时流式重新加密页首块，未修改归档重写保持 **100% 字节级一致**。
- **非标准种子变体（90CN）与种子恢复**：
  - 内置识别常见固定变体密钥常数（如 90CN 的 Header、GRPI、Body、sTrA、sTrW 段种子）；
  - 具备未知变体自动反解算法，利用 `nkpi` 明文签名约束与 zlib 流特征快速推导全部密钥流；
  - 保存时智能保留变体原始密文 HASH 段或按结构重建，确保新增文件可被客户端正常索引。
- **智能字符串池选择与编码防乱码**：
  - 严格遵循客户端约定：写入中文等非 ASCII 脚本字符串时自动路由进入 `sTrW`（UTF-16LE 奇数偏移），ASCII 遵循池约定，彻底根除新写脚本在游戏内乱码的问题；
  - 针对 DataType 3 本地化文本，写回时自动识别原载荷特征，保留原 EUC-KR / CP437 伪画编码映射，确保读写完全一致。

### 🌲 智能资源管理器
- **树状目录懒加载**：分层动态展开，实时显示各目录下的子节点统计数量。
- **修改状态动态标记**：直观标记已修改文件（Dirty 状态）与变动目录，修改范围一目了然。
- **游标分页检索（Cursor Pagination）**：支持在百万级路径中进行全量不区分大小写的模糊搜索与精确匹配，滚动按需加载，输入防抖平滑无顿挫。
- **名称解析与未翻译标记**：支持 110US 占位符解析，文件树节点与搜索结果直接显示中文化名称；对仅由韩文/回退覆盖层命中的条目自动标记 `（未翻译）`。
- **智能预览入口分派**：仅对支持预览的归档类型与对应资源文件展示预览入口，避免在不支持的文件类型中展示无效操作。
- **便捷文件与目录操作**：支持单击/双击打开行为自定义；右键菜单提供复制相对/绝对路径二级菜单；通过脚本或新建文件时，自动按需递归补全父级目录；已注册 `.pvf` 文件关联，支持系统双击或拖拽文件直接打开归档。

### 📝 结构化反编译与专业级编辑
- **脚本反编译（DataType 1）**：
  - 深度解析 5-byte Token 流，无损反编译为语义清晰的脚本代码。
  - **层级感知缩进**：自动根据代码段落（`[tag]` 至 `[/tag]`）计算嵌套缩进深度。
  - **可配置渲染规则**：内置 `skill data up`（7 token/行）、`skill levelup`（3 token/行）、`.lst`（2 token/行）等换行排版规则；也可通过 `rendering.json` 按扩展名、glob 与 section 自定义 `offset` 及每行 token 数，设置面板支持热重载。
  - **完整语法支持**：支持行内串 `` `...` ``、块串标记 `{5=`...`}` 与 `{7=`...`}`，支持单行注释 `#`。
  - **110US 占位符语法支持**：全面解析 DataType 8（行内引用）与 DataType 10（多行命令文本）占位符 Token（如 `{8=`<31::equip_name_1>`}`）。
- **110US 字符串表与占位符就地编辑**：
  - 代码内嵌占位符译文 Tag，悬停提示原始占位符与来源 `.str` 字符串表路径；
  - **就地修改译文**：直接单击占位符标签即可唤起编辑弹窗，按 UTF-16LE 字节局部改写对应字符串表条目（即使 50MB 巨型字符串表亦无需全量载入），改动即刻同步刷新编辑器、文件树与搜索索引；
  - **新建文件条目创建**：新建脚本时支持创建新的字符串表条目并一键插入占位符引用。
- **本地化文本编码修复（DataType 3）**：
  - 针对韩服转制特有的「EUC-KR 字节被逐字节按 CP437 字体映射进 UTF-16」的历史遗留乱码，提供内置自动识别与还原修复（还原为标准的 CP949 / EUC-KR 文本）。
- **专业级代码编辑体验**：
  - 基于 **CodeMirror 6** 深度定制，内置行号、活动行高亮、括号匹配与行内查找替换；<kbd>Tab</kbd> 键插入标准制表符。
  - **分屏编辑**：支持水平（左右并排）与垂直（上下并排）多窗格分屏，自由拖拽拆分条调整面板尺寸。
  - **Vim 编辑模式**：可在设置中随时启用 Vim 键位绑定，键盘流操作丝滑流畅。
  - **深浅色主题自适应**：原生支持深色（Dark）、浅色（Light）及跟随系统自动切换。
  - **多标签页管理**：支持标签页批量关闭（关闭右侧/关闭其他/全部关闭）；编辑内容手动保存（工具栏保存按钮或 <kbd>Cmd</kbd>/<kbd>Ctrl</kbd>+<kbd>S</kbd>）后写入内存修改 Overlay，关闭未保存的标签前会提示确认。
  - **Section 折叠**：按脚本 section 折叠代码，便于浏览较长文件。

### 🏷️ 智能标注系统与 LST / IndexHash 管理
- **规则驱动关联引擎**：通过灵活的 JSON 标注规则，自动关联装备、道具、技能等 `.lst` 列表映射配置与引用关系。
- **双布局列表自适应**：同时兼容 90US 目录相对路径与 110US 归档根相对路径（`list/` 集中布局）。
- **`list/*_indexhash.etc` 解析与自动注册**：原生解析 110US 列表伴生的 indexhash 文件，内置双轮 uint32 混合哈希计算算法，提供可视化的 `hashindex` 自动生成与列表注册界面，确保新增物品正常被游戏索引。
- **内嵌交互标注 Tag**：在脚本关键 ID 旁直观展示对应名称，支持设置 Tag 摆放位置（跟随目标后、行尾固定或隐藏）。
- **一键跨文件跳转**：按住 <kbd>Cmd</kbd> / <kbd>Ctrl</kbd> 单击标注标签，直接在编辑器中定位并打开被引用的目标脚本文件。
- **可控标注显示**：支持路径定位与跳转、临时关闭标注、隐藏标注时保留悬停提示，并按物品稀有度着色名称。
- **独立标注编辑器**：内置 `cmd/annotation-editor` 工具，支持可视化调试与编辑标注规则。

### 🖼️ 丰富的资源预览（ANI 动画 / 装备面板 / NPK 贴图）
- **ANI 动作动画预览**：深度解析 `.ani` 关键帧序列、每帧延迟（delayMs）与贴图引用图层，内置播放器支持循环播放、阴影开关与逐帧步进控制，在编辑器内直接播放角色技能、怪物动作及技能特效。
- **装备属性面板渲染**：深度解析 `.equ` 装备脚本，渲染贴近游戏实际体验的高保真属性面板，展示装备品级、穿戴等级、职业限制、基础四维、物理/魔法攻击及技能等级加成。
- **套装效果预览**：在装备属性面板中展示关联套装的属性与效果。
- **统一预览宿主（PreviewHost）**：根据当前打开文件类型（`.ani`、`.equ`、`.img` 等）自动分派预览组件，无缝集成在编辑器侧边。
- **高性能 NPK 解析内核**：纯 Go 实现的只读 NPK / IMG 解码器，秒级扫描并索引数万张贴图资源。
- **DXT 纹理解码**：原生支持 DXT1、DXT3、DXT5 压缩纹理解压，以及 1555、4444、8888 等常见像素格式。
- **行内贴图与悬停预览**：脚本中的贴图标注支持直接嵌入 16x16 行内缩略图；鼠标悬停标签时弹出清晰大图预览卡片。

### 🗂️ 结构化数据 GUI 编辑（商店 / 基础掉率 / 全局掉落）
- **文件 GUI 模式框架**：文件默认以文本打开，命中 GUI provider 后才出现模式切换入口；显示模式归属窗格，每个窗格独立保存，隐藏时保留组件状态、关闭文件时才销毁（文本编辑器采用隐藏而非卸载，保留光标、滚动与撤销历史）。provider 通过 `frontend/src/gui/registry.ts` 以唯一 `id`、显示名称、`readOnly` 能力、文件匹配条件与异步 Vue 组件注册；异步读取必须校验归档 epoch 与内容 revision，防止旧请求覆盖新内容。
- **商店查看与编辑（`.shp`）**：保留 Tab、分类区块与商品出现顺序，不合并重复商品；区块与商品记录携带 UTF-16 源位置，支持精确修改或删除某一次商品出现。商品成本来自商品文件本身（`[price]` 金币、`[need material]` 成对的道具 ID 与数量），两类成本可共存，缺失成本与显式零金币语义不同，未知引用与异常结构返回数据提示、缺图使用占位。
- **商品与分页管理**：商品 hover 或键盘聚焦后显示编辑、删除按钮，更换商品 ID 会先读取新商品成本再允许修改，删除只移除当前出现而不删除物品文件。底部提供添加商品、分页管理与批量设置入口：分页管理支持新增、重命名与删除（删除分页会移除其中全部分类的引用）；批量设置按关联物品文件去重覆盖当前分页全部大分类，金币与兑换材料可分别选择保持或替换，空金币移除 `[price]`、空材料移除 `[need material]`。
- **通用装备/道具选择器**：`ItemPicker` 支持图标、ID/名称/路径检索、延迟搜索与分页，并隔离过期请求；`ArchiveService.SearchItems` 在内存与 SQLite 索引中均先限定物品范围再分页。
- **基础掉率编辑器**：可视化编辑 `basis of rarity dicision` 各分组（每 7 项一组、5 个数值）的掉率，面向 90US / 90CN 归档；保存前校验数据代次，避免过期表单覆盖最新内容。
- **全局掉落编辑器**：对 `etc/worlddrop.etc` 按等级管理道具及权重，可在 GUI 与文本模式间切换；提交前校验当前文件与归档版本，避免覆盖较新的修改。
- **安全提交语义**：编辑请求先校验归档 revision、文件路径与 UTF-16 定位，再在独立 staging 中构造全部受影响文件，全部校验通过才一次性提交 overlay，并记录为一次版本撤销操作；原 PVF 不会自动写盘，仍需手动保存，过期表单会被拒绝而不覆盖更新后的内容。

### 🗃️ 资源版本控制（VCS）
- **本地旁路版本库**：在 PVF 归档旁自动创建 `.pvfine` 侧边版本库，基于 SQLite 元数据与 SHA-256 CAS 不可变对象存储。
- **版本历史与提交**：直观的版本控制面板，支持查看工作区改动清单、填写提交说明并生成版本快照（支持快捷键提交）。
- **快照比对与安全检出**：支持对比历史快照改动差异，一键回滚未提交改动或检出历史版本，不破坏原 PVF 文件结构。

### 📑 嵌套书签簿与文件集持久化
- **嵌套书签簿**：支持创建多级树状嵌套书签簿，自由归类、编辑与重命名常用脚本，点击即刻直达。
- **文件集持久化（FileSets）**：支持将跨目录的相关文件编组收藏，配置自动持久化保存，支持一键批量导出选中文件集。
- **最近打开记录**：自动追踪最近编辑与访问的历史文件，快速重新载入。

### 🧩 JavaScript 脚本工作区
- **沙箱化脚本引擎**：进程内集成 Goja 运行时，无需 Node 环境即可执行 `.pvf.js`；提供 `pvf.files` / `find` / `glob` 文件遍历、`text()` 文本读写、`parse()` 结构化 Token 文档 API，以及 `pvf.fileset` / `createFileset` 文件集读写。
- **丰富的文件与列表 API**：提供 `.lst` 列表遍历（`forEach`）、查询与条目改写 API；提供文件新建、复制、删除与自动补建父目录能力。
- **事务隔离与预览确认**：运行阶段的写入全部落在隔离事务中，成功后生成按文件分页的可审阅 Diff 预览（支持按路径筛选）；只有点击「应用选中」才会写入工作区 Overlay，且仍需手动保存归档。
- **安全边界**：沙箱不注册 `require`、`process`、网络、本地文件与 shell 能力；单次运行上限 5 分钟，同时仅允许一个运行实例，停止、超时、切换归档或运行异常均自动回滚。
- **脚本内联编辑与独立窗口**：编辑面板内提供 JavaScript 语法高亮、脚本 API 类型声明与代码补全；支持将脚本工作区以独立窗口打开，便于并排多任务操作。

### ⚡ 批处理与外部资源导入
- **Token 级批处理引擎**：支持针对 5-byte Token 树和 Section 节点的批量修改规则执行，提供变更数量统计与可视化的 Diff 差异对比预览，确认无误后再安全应用。
- **外部资源一键导入**：支持从本地文件系统批量导入文件与目录至归档指定目录，智能识别脚本/原始二进制，提供冲突与覆盖预览。

### 🛡️ 增量打包、原子落盘与退出防护
- **Dirty-chunk 差异重打包**：
  - 保存时自动比对修改分块，仅对产生变动的 Chunk 重新执行 zlib 压缩与 `"BodY"`（或 Paged110 `"mAIn"`）加密，未改动分块直接复用原始密文。
  - 自动维护并追加更新 `sTrA`（UTF-8）与 `sTrW`（UTF-16LE）双字符串池及客户端检索二分 `HashTable`。
- **字节级一致性验证**：未修改的归档（包括标准 90US 与 110US Paged110 分页归档）经重打包后，与原始文件保持 **100% 逐字节完全一致**（已通过真实归档交叉单测）。
- **保存确认与自动备份**：覆盖保存源文件时弹出确认弹窗（带加载中状态反馈），并支持自动将原文件备份为 `.bak`。
- **原子保存与退出防丢**：数据先写入临时文件（`*.pvftmp`）再执行 `os.Rename` 原子覆盖；窗口关闭时提供未保存修改拦截防护（CloseGuard），杜绝数据丢失。
- **缓存占用与一键清理**：系统维护设置页在打开时统计搜索索引、归档索引、高级搜索索引、NPK 图标索引与定时缓存的总占用，并可一键清理这些可重建的缓存；设置、书签与文件集等配置数据不在缓存目录内，不受影响。
- **定时缓存与异常恢复**（可选，默认关闭）：按设置的间隔把当前工作区（含未保存修改）从隔离克隆另存为一份缓存 PVF（默认位于系统缓存目录），不改变工作区的“未保存”状态、也不写入原文件；崩溃或被强制结束后重新启动会提示恢复，恢复后工作区仍为未保存状态。手动保存成功、退出时放弃修改或选择“丢弃备份”后，缓存自动删除。

### 📦 提取与整包解包
- **单文件与批量导出**：支持一键导出当前文件或按选定范围批量导出原始二进制文件。
- **流式全量解包**：支持在后台协程将整包数万/数百万文件并发解压导出至指定目录，前端状态栏实时进度条显示，支持随时优雅取消。

---

## ⌨️ 快捷键操作

工作区快捷键可在偏好设置中自定义、清除或恢复默认值；设置时会检测冲突及系统、编辑器保留组合键。

| 快捷键 (macOS) | 快捷键 (Win / Linux) | 功能说明 |
|:---|:---|:---|
| <kbd>Cmd</kbd> + <kbd>O</kbd> | <kbd>Ctrl</kbd> + <kbd>O</kbd> | 打开 PVF 归档文件 |
| <kbd>Cmd</kbd> + <kbd>S</kbd> | <kbd>Ctrl</kbd> + <kbd>S</kbd> | 保存全部修改至原归档（附备份确认）；脚本工作区内保存脚本 |
| <kbd>Cmd</kbd> + <kbd>Shift</kbd> + <kbd>S</kbd> | <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>S</kbd> | 另存为新 PVF 文件 |
| <kbd>Cmd</kbd> + <kbd>W</kbd> | <kbd>Ctrl</kbd> + <kbd>W</kbd> | 关闭当前编辑器标签页 |
| <kbd>Cmd</kbd> + <kbd>Shift</kbd> + <kbd>W</kbd> | <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>W</kbd> | 关闭其他标签页 |
| <kbd>Cmd</kbd> + <kbd>Alt</kbd> + <kbd>W</kbd> | <kbd>Ctrl</kbd> + <kbd>Alt</kbd> + <kbd>W</kbd> | 关闭全部标签页 |
| <kbd>Cmd</kbd> + <kbd>\</kbd> | <kbd>Ctrl</kbd> + <kbd>\</kbd> | 左右拆分编辑器（分栏分屏） |
| <kbd>Cmd</kbd> + <kbd>Shift</kbd> + <kbd>\</kbd> | <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>\</kbd> | 上下拆分编辑器（多行分屏） |
| <kbd>Cmd</kbd> + <kbd>Enter</kbd> | <kbd>Ctrl</kbd> + <kbd>Enter</kbd> | 提交当前版本快照；脚本工作区内运行脚本预览 |
| <kbd>Cmd</kbd> + <kbd>Shift</kbd> + <kbd>F</kbd> | <kbd>Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>F</kbd> | 打开高级搜索 |
| <kbd>Cmd</kbd> + <kbd>,</kbd> | <kbd>Ctrl</kbd> + <kbd>,</kbd> | 打开偏好设置 |
| <kbd>Cmd</kbd> + <kbd>Click</kbd> | <kbd>Ctrl</kbd> + <kbd>Click</kbd> | 单击标注标签快速跳转引用脚本；单击占位符标签就地修改译文 |
| <kbd>Cmd</kbd> + <kbd>F</kbd> | <kbd>Ctrl</kbd> + <kbd>F</kbd> | 编辑器内查找与替换 |

---

## 🏗️ 系统架构

本项目采用严格关注点分离的模块化架构：

```
┌────────────────────────────────────────────────────────────────────────┐
│                      Frontend (Vue 3 + Naive UI)                       │
│  - ToolBar: 归档生命周期 / 导入 / 解包 / 搜索 / 版本 / 侧栏切换        │
│  - Explorer: 虚拟树形懒加载 / 修改标记 / 路径复制 / 游标分页搜索       │
│  - CodeEditor: CodeMirror 6 / Vim 模式 / LST 标注与贴图 / 分屏协同     │
│  - Previews: PreviewHost 统一宿主 / AniPreview 动画 / EquipmentPreview │
│  - FileGUI: GUI Provider / 商店与全局掉落编辑 / 装备道具选择器         │
│  - Sidebars & Modals: 占位符修改 / IndexHash 注册 / 批处理 / 设置 ...  │
│  - Pinia Stores: archive / editor / fileGUI / dropRate / autosave ...  │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Wails v3 IPC (Auto Bindings)
┌───────────────────────────────────▼────────────────────────────────────┐
│                    Services Layer (pvfine/services)                    │
│  - ArchiveService: 归档加载、生命周期、目录树与检索、资源导入与解包    │
│  - EditorService: 文本反编译、Overlay 缓存、落盘原子保存、导出与备份   │
│  - PreviewService: ANI 关键帧动画解析、装备属性面板计算与预览分派      │
│  - ListRegistration: 列表关联注册与 list/*_indexhash.etc 自动生成     │
│  - SearchIndexCache: 归档元数据与搜索索引持久化缓存                    │
│  - AdvancedSearchSQLite: 字符串反向索引、分页查询与结果磁盘缓存        │
│  - SearchMutation: 变更影响分类与搜索索引增量刷新                      │
│  - VersionService: 本地版本库生命周期、提交、快照差异与检出            │
│  - AnnotationService: 规则引擎绑定、LST 索引构建与关联计算             │
│  - ImageService: NPK 资源索引、DXT 图像解码与缩略图缓存                │
│  - BatchService: 批处理规则变换、语法树扫描与 Diff 差异生成            │
│  - ScriptService: Goja 沙箱执行、结构化脚本预览与选择性应用            │
│  - RenderingService: 渲染规则校验、热重载与编辑器排版配置              │
│  - BookmarkService & FileSetService: 嵌套书签簿与文件集持久化          │
│  - FileGUIService: 商店与全局掉落读取、编辑校验与提交                   │
│  - DropRateService: 基础掉率分组读取与写回                             │
│  - AutosaveService: 定时工作区缓存与异常退出恢复                       │
│  - CacheService: 可重建缓存占用统计与一键清理                          │
│  - SettingsService: 全局用户配置 (主题、打开方式、Vim、备份等)         │
│  - Core: 读写锁守卫的共享并发状态模型                                  │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Go API
┌───────────────────────────────────▼────────────────────────────────────┐
│                              Core Engines                              │
│  - internal/pvf: 90US/110US(Paged110) 容器/变体恢复/Token/字符串池/表 │
│  - internal/preview: ANI 关键帧序列与图层调度模型                      │
│  - internal/script: Goja 沙箱运行时与事务化 PVF 宿主 API               │
│  - internal/rendering: 渲染规则编译与编辑器展示格式解析                │
│  - internal/npk: NPK 容器读取、IMG 图像帧解析与 DXT1/3/5 解码          │
│  - internal/annotations: 规则加载、版本过滤、LST 双布局映射与联合索引  │
│  - internal/version: SQLite 版本库管理与 SHA-256 CAS 对象存储          │
└────────────────────────────────────────────────────────────────────────┘
```

> 详细的 PVF 文件布局、110US 容器分析与解密算法数学推导，请参阅 [docs/FORMAT.md](docs/FORMAT.md) 与 [docs/PVF新格式分析.md](docs/PVF新格式分析.md)。

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

开发构建只对本项目（`pvfine/...`）禁用函数内联，保留业务代码的调试能力；SQLite 等依赖保持编译优化。不要使用 `-gcflags=all=-l` 做性能对比，它会显著拖慢纯 Go SQLite。复测开发版索引性能时，使用相同的 `-gcflags="pvfine/...=-l"` 参数。

如需仅对前端进行调试：

```bash
cd frontend
npm install
npm run dev
```

### 运行测试与基准测试

运行核心内核与服务层单元测试（涵盖 PVF、Paged110 分页归档、90CN 变体与种子恢复、HASH/indexhash、字符串表与编码防乱码、ANI/装备预览、NPK、标注与版本过滤、脚本沙箱、渲染规则、商店编辑、基础掉率、缓存与自动保存、版本控制及应用服务）：

```bash
go test ./...
```

运行前端单元测试（Vitest，覆盖编辑器加载与标注、文件 GUI、商店表单、基础与全局掉率、快捷键及自动保存）：

```bash
cd frontend
npm test
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
│   ├── pvf/                  # PVF / Paged110 容器、变体与种子恢复、反编译、字符串池/表、IndexHash、差异重打包
│   ├── preview/              # ANI 关键帧序列解析与动画模型
│   ├── script/               # Goja 沙箱与仅面向事务的 PVF 宿主 API（支持列表、文件集与增删改）
│   ├── npk/                  # NPK 资源包读取、IMG 图像帧解析与 DXT1/3/5 解码器
│   ├── annotations/          # 标注规则引擎、LST 双布局映射与装备/道具联合索引
│   └── version/              # 本地版本库模型、SQLite 元数据与 SHA-256 CAS 对象存储
├── services/                 # 应用服务层：桥接 Go 内核与前端 IPC 状态
│   ├── core.go               # 线程安全共享 Core、目录树索引与搜索状态
│   ├── archive.go            # ArchiveService：归档打开、懒加载目录树、游标搜索
│   ├── editor.go             # EditorService：文本反编译、内存编辑、保存与解包
│   ├── preview.go            # PreviewService：ANI 动画与装备属性预览解析分派
│   ├── equipment_preview.go  # 装备脚本高保真属性面板提取与计算
│   ├── list_registration.go  # 列表注册与 list/*_indexhash.etc 维护
│   ├── search_index_cache.go # 归档元数据与搜索索引持久化缓存
│   ├── advanced_search_sqlite.go # 高级字符串反向索引与查询结果缓存
│   ├── sqlite_index.go       # 文件索引与语义索引的 SQLite 构建
│   ├── search_mutation.go    # 归档变更影响分类与搜索索引增量刷新
│   ├── annotations.go        # AnnotationService：标注查询与 LST 关联
│   ├── batch.go              # BatchService：批处理规则解析与 Diff 预览
│   ├── script.go             # ScriptService：脚本运行、预览计划、脚本目录与应用
│   ├── script_filesets.go    # 脚本内文件集操作支持
│   ├── bookmarks.go          # BookmarkService：嵌套书签簿增删改查
│   ├── filesets.go           # FileSetService：文件集持久化管理
│   ├── image_service.go      # ImageService：NPK 资源管理与图像缓存
│   ├── import.go             # 资源批量导入与冲突预览
│   ├── file_gui.go           # FileGUIService：商店文档读取与编辑校验提交
│   ├── shop_edit.go          # 商店商品/分页编辑、批量设置与表单校验提交
│   ├── drop.go               # DropRateService：基础掉率分组读取与写回
│   ├── autosave.go           # AutosaveService：定时工作区缓存与崩溃恢复
│   ├── cache.go              # CacheService：可重建缓存占用统计与一键清理
│   ├── settings.go           # SettingsService：全局用户配置与偏好设置
│   ├── version.go            # VersionService：版本仓库控制与快照管理
│   └── window.go             # 窗口服务（支持脚本工作区分离独立窗口）
├── frontend/                 # 前端工程：Vue 3 + TypeScript 响应式桌面 UI
│   ├── src/
│   │   ├── components/       # UI 组件 (ToolBar, Explorer, EditorTabs, EditorPane,
│   │   │                     #         BookmarkSidebar, FileSetSidebar, VersionPanel, BatchProcessModal,
│   │   │                     #         ScriptWorkbench, CodeEditor（PVF / JavaScript 双模式）,
│   │   │                     #         previews/ (AniPreview, EquipmentPreview, PreviewHost),
│   │   │                     #         gui/ (FileGUIHost, ShopViewer, ShopEditDialog, ShopCostFields),
│   │   │                     #         DropRateEditorModal, ItemPicker, RecoveryPrompt,
│   │   │                     #         IndexHashRegistrationModal, AdvancedSearchModal, ImportModal, SettingsModal, StatusBar, CloseGuard)
│   │   ├── gui/              # 文件 GUI provider 注册表、状态模型与商店表单
│   │   ├── stores/           # Pinia 状态管理 (archive, explorer, editor, fileGUI, dropRate,
│   │   │                     #                 autosave, bookmarks, fileSets, version, images,
│   │   │                     #                 settings, batch, script, advancedSearch, import)
│   │   ├── theme.ts          # 深色 / 浅色 / 跟随系统主题配色体系
│   │   ├── App.vue           # 主界面布局、侧边栏集成与全局快捷键监听
│   │   └── main.ts           # 前端入口
│   ├── tests/                # Vitest 前端单元测试（编辑器 / 文件 GUI / 商店 / 掉率 / 自动保存）
│   └── bindings/             # Wails 自动生成的 TypeScript 服务端点绑定
├── docs/                     # 技术规格文档
│   ├── FORMAT.md             # S4A21 PVF 二进制格式逆向分析规格与数学算法
│   ├── PVF新格式分析.md       # 110US (Paged110) 容器结构、sk.dat 密钥推导与字符串表分析
│   ├── 脚本工作区设计.md     # Goja 沙箱 API、事务边界与预览应用流程
│   ├── 渲染规则设计.md       # 渲染规则 JSON 结构与匹配优先级
│   ├── 文件GUI模式.md        # GUI provider 扩展、商店数据结构与编辑检索流程
│   ├── 标注功能设计.md       # 标注系统规格与规则配置
│   └── 版本控制设计.md       # 本地版本库设计
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

本项目基于 [GNU General Public License v3.0 (GPL-3.0)](LICENSE) 协议开源。
