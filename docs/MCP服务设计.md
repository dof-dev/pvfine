# MCP 服务设计

状态：首版已实现。工具定义以 services/mcp_tools.go 为准，下文保留设计约定与实现边界。

当前实现：设置页启停及复制连接配置、本机 Streamable HTTP、共享服务实例、异步任务/状态查询、脚本预览应用、归档生命周期、冻结导入及普通/MOD 导出。界面准备屏障覆盖主窗口与脚本窗口，归档写入后同步标签内容和后端基线再释放屏障，避免将已应用变更误判为本地草稿。script_help 的 types 主题嵌入编辑器同源类型声明。连接无需鉴权，本机连接共享任务与计划。已实现标注搜索、详情和实际文件解释，见 [标注文档协议](MCP标注文档协议.md)。

当前限制：任务/请求去重历史仅保存在进程内，最多 256 条，重新启用清空；导入最多 10000 文件、单文件 8 MiB、总计 64 MiB；导出最多 10000 文件、内容 128 MiB。大导入快照保存在受限内存中，超限拒绝；尚未使用磁盘溢出存储。预览有效期 10 分钟，差异与日志有响应大小限制。生命周期和导出发布阶段不支持取消。

## 目标与首版范围

AI 客户端连接已运行的 pvfine，与桌面界面共享当前 PVF，支持打开、关闭、保存、另存为、普通文件/目录导入、普通导出、导出为 MOD，以及搜索、读取和修改归档内脚本。复用现有 `.pvf.js` 执行能力完成复杂查询和批量变更。

首版通过已运行的桌面应用管理归档生命周期及文件导入导出，AI 可直接传入本机归档路径和来源/目标路径，无需弹出文件选择框。用户已确认导入范围为普通文件和目录，不含 MOD 包导入。首版不提供无界面的独立归档进程、任意磁盘读写或系统命令工具。应用归档变更或导入后，可由 AI 调用保存/另存为，也可通过现有界面保存。

这里区分三种数据：归档内脚本、用于操作归档的 `.pvf.js` 程序，以及持久化在用户配置中的文件集。接口及结果必须说明变更的落点。

## 架构与传输

```text
AI MCP 客户端
    │ Streamable HTTP /mcp
    ▼
internal/mcpserver：协议、身份校验、输入输出约束
    ▼
services：面向 AI 的应用门面、任务与预览管理
    ├── ArchiveService / EditorService：归档生命周期、搜索和读取
    ├── ScriptService：编译、运行、预览和应用
    ├── 导入门面 / ExportService：文件导入、普通导出及 MOD 导出
    └── 共享 core：当前归档、版本、Overlay、事件
```

MCP Server 随桌面应用启动和停止，设置中提供启停开关，默认关闭。启用后使用本地 Streamable HTTP 端点，直接注入界面使用的服务实例；尤其必须共享同一个 ScriptService，避免独立实例绕过运行互斥。

使用官方 `github.com/modelcontextprotocol/go-sdk`。实现时选择与项目 Go 版本兼容的稳定版本，依靠 SDK 处理协议版本兼容。MCP 2026-07-28 与旧版本在生命周期和取消语义上存在差异，应用层统一使用任务 ID 和 context，避免自行固化某版协议细节。

固定首选端口可在设置中配置；端口占用时明确报错，界面显示实际端点和连接状态。多个应用实例使用不同端口，并通过 workspace 身份避免操作错归档。仅支持 stdio 的客户端可在后续通过轻量桥接进程连接已有服务，首版不增加桥接进程。

## 首版工具

workspace_info 与首次打开归档允许在空工作区调用。其余归档相关操作携带 `workspaceId`；读取返回 `revision`，执行、应用与生命周期写操作携带预期 revision。分页读取后 revision 变化时要求重读，避免混合不同版本的数据。生命周期另外使用 workspaceEpoch 防止空工作区检查与打开提交之间的竞争。

| 工具 | 输入要点 | 输出与语义 |
| --- | --- | --- |
| `pvf_workspace_info` | 无 | 是否打开、workspaceEpoch、workspaceId、源路径、归档格式、revision、源文件未保存状态、各窗口草稿状态、运行任务、能力版本 |
| `pvf_search` | workspaceId、路径/内容模式、query、scope、cursor、limit | 路径、命中摘要、revision、后续游标；限制单页与总响应大小 |
| `pvf_read_file` | workspaceId、path、行范围、expectedRevision | 脚本文本、总行数、revision、截断状态；路径使用归档内部路径 |
| `pvf_script_help` | topic | 分主题返回 API 类型、说明和示例；不依赖客户端支持 resources |
| `pvf_script_validate` | source | 语法诊断；不会验证业务含义或游戏效果 |
| `pvf_script_run` | workspaceId、expectedRevision、source、label、requestId | 立即返回 taskId；脚本在隔离事务中执行 |
| `pvf_script_status` | taskId、日志游标 | 状态、进度、分页日志、诊断；完成后返回 planId 和变更摘要 |
| `pvf_script_preview` | planId、filter、cursor、limit | 分页差异、changeKey、警告及依赖信息 |
| `pvf_script_apply` | planId、expectedRevision、changeKeys、requestId | 应用明确选中的变更，返回新 revision 和修改统计 |
| `pvf_script_cancel` | taskId | 取消指定任务；不能误取消界面或其他连接发起的任务 |
| `pvf_script_discard` | planId | 释放指定预览及其隔离事务 |
| `pvf_archive_open` | path、expectedWorkspaceEpoch、当前 workspaceId/revision（如有）、dirtyPolicy、requestId | 打开或切换归档；返回 taskId，完成后获取新 workspaceId、源路径、revision 与初始化状态 |
| `pvf_archive_close` | workspaceId、expectedRevision、dirtyPolicy、requestId | 关闭当前归档；返回 taskId，完成后报告空工作区及新 workspaceEpoch |
| `pvf_archive_save` | workspaceId、expectedRevision、requestId | 保存到源路径；返回 taskId，完成后报告 writtenPath、源文件未保存状态及文件标识 |
| `pvf_archive_save_as` | workspaceId、expectedRevision、path、overwrite（默认 false）、目标文件标识（覆盖时）、requestId | 生成副本并保留源路径；返回 taskId，完成后同时报告 writtenPath、sourcePath 与源文件未保存状态 |
| `pvf_task_status` | taskId | 查询生命周期、导入导出任务进度、结果和错误；与脚本任务共用底层任务管理，保留 script_status 的专用视图 |
| `pvf_import_prepare` | workspaceId、expectedRevision、sourcePaths、targetDir、mode、requestId | 冻结本地文件内容并校验映射，返回 taskId；完成后取得 importPlanId 与新增/覆盖摘要 |
| `pvf_import_preview` | importPlanId、cursor、limit | 分页返回源路径、归档目标路径、类型、大小、覆盖状态及差异 |
| `pvf_import_apply` | importPlanId、expectedRevision、selectedKeys、overwriteKeys、requestId | 将明确选择的冻结内容应用到工作区，不保存源 PVF；返回 taskId 和最终 revision |
| `pvf_import_discard` | importPlanId | 释放冻结内容与预览 |
| `pvf_export_formats` | 无 | 格式 ID、读写能力与支持操作，首版可写 MOD 格式为 110USextend |
| `pvf_export_prepare` | workspaceId、expectedRevision、source、scopes、mode、format、name、version、includeDependencies、来源附加参数、requestId | 准备普通/MOD 导出快照，返回 taskId；完成后取得 exportPlanId、总数与警告 |
| `pvf_export_preview` | exportPlanId、cursor、limit | 分页输出树、entryKey、依赖提示、跳过项与警告 |
| `pvf_export_execute` | exportPlanId、outputParent、outputName（direct 必填）、selectedEntryKeys、acknowledgedWarningIds、requestId | 发布普通文件目录或 MOD 目录；返回 taskId，完成后返回实际输出路径和统计 |
| `pvf_export_discard` | exportPlanId | 释放导出快照 |

`pvf_script_run` 同时支持只读查询：脚本用 `pvf.log` 输出查询结果，没有变更时不保留无用预览。大型查询优先使用搜索/读取工具，避免把所有结果堆入日志。首版不新增脚本 return/结果集语言语义。

通过 `pvf_script_help` 与可选 MCP resources 暴露相同的分主题文档，例如 `pvfine://script-api/sections`。文档应和 `frontend/src/script-api.d.ts`、脚本能力版本一起维护，并提供一致性检查。归档路径和内容是数据，不作为服务器指令执行。

`.pvf.js` 程序库的列出、读取和保存可后续映射现有 ListScripts/LoadScript/SaveScript；首版通过 source 执行，不默认改写界面中尚未保存的脚本源码。

## 典型调用流程

1. AI 查询工作区；需要时调用 archive_open 打开指定路径，获取 workspaceId 和 revision，搜索候选文件并读取样本。
2. 按需查询脚本 API，生成 source，验证语法。
3. 提交执行任务；服务在接纳任务时记录任务身份和预期 revision。
4. AI 查询进度与运行结果，读取差异及警告。
5. AI 按明确的 changeKeys 应用结果；revision 已变化则拒绝并要求重新运行。
6. UI 刷新受影响文件并显示未保存状态，AI 调用 archive_save/archive_save_as，或用户使用原有保存入口写回 PVF。
7. 结束后可调用 archive_close；有未保存内容时按显式 dirtyPolicy 处理。

导入分支：import_prepare → import_preview → import_apply → 按需保存。导出分支：export_prepare（mode=direct/mod）→ export_preview → export_execute。两种导出都不要求先保存 PVF。

例如“把某目录价格低于 10 的装备提高到 10”，可直接复用脚本工作区的 `pvf.glob → file.parse → section.set → file.write`。工具描述应提示保留数值类型、显式处理重复 section，并检查字符串表与列表依赖。

## 归档生命周期

### 打开与切换

`pvf_archive_open` 接受本机绝对文件路径，不弹对话框，不自动猜测路径。空工作区使用 workspace_info 返回的 expectedWorkspaceEpoch；已有归档时还必须匹配 workspaceId 和 expectedRevision。同一路径重新打开也视为新工作区，不能绕过未保存检查。

打开前取得生命周期操作占用权，在候选归档上完成解析和必要索引准备，成功后才替换当前工作区；失败保留原归档、Overlay 和草稿。切换提交前再次校验工作区身份、revision 和草稿状态，防止解析期间 UI 编辑造成覆盖。后台版本恢复和索引任务按工作区代次过滤，不能将旧归档结果装入新工作区。

成功返回新的 workspaceId、workspaceEpoch、revision，以及 indexStatus/versionStatus。打开完成不等于后台索引和版本恢复完成；未就绪操作返回 NOT_READY，并说明可查询的状态。

### 未保存内容与关闭策略

open/close 的 dirtyPolicy 支持 `reject`（默认）、`save`、`discard`，只处理当前归档的已同步 Overlay：

| 策略 | 当前源文件有未保存变更时的行为 |
| --- | --- |
| reject | 返回 UNSAVED_CHANGES，保留当前工作区 |
| save | 先保存到当前源文件，成功后再切换/关闭；保存失败则保留当前工作区 |
| discard | 明确丢弃当前 Overlay 后切换/关闭；仍需匹配身份和 revision |

这些策略不隐式应用脚本预览，不清除前端未提交草稿。存在前端草稿时统一返回 UI_DRAFT_PENDING，要求在现有编辑器提交或处理草稿后重试。`discard` 不能绕过草稿保护。独立脚本窗口的未保存 `.pvf.js` 源码属于程序草稿，切换/关闭归档应保留，不将其计入归档 Overlay。

运行中的脚本或已有待处理预览分别返回 TASK_BUSY / PLAN_PENDING，先通过既有工具取消、应用或丢弃，再操作归档；不能通过 close/open 隐式终止其他调用方的任务。生命周期操作互斥，同时阻止 UI/MCP 开始新的归档写入或脚本任务。

close 成功释放归档资源、索引与版本会话，并按现有语义清理该源路径的自动备份；桌面应用与 MCP Server 继续运行，允许再次打开。workspaceEpoch 更新，旧工作区句柄失效。同一 requestId 的重试返回原关闭结果；新请求引用旧身份返回 STALE_WORKSPACE。

### 保存

`pvf_archive_save` 保存后端已提交的当前 revision 到 sourcePath。前端仍有文本或 GUI 草稿时返回 UI_DRAFT_PENDING，不返回遗漏草稿的“保存成功”。没有源路径时返回 SAVE_AS_REQUIRED。保存使用现有原子写入、源文件备份、版本记录和索引更新流程。

保存前比较磁盘文件与打开或上次成功保存时的文件标识；不一致返回 SOURCE_CHANGED，禁止覆盖外部修改。文件标识包含必要的文件身份和内容校验，不能只依赖 mtime。文件替换前再次检查；对于不遵守应用锁的外部写入，普通文件系统检查不等于跨进程事务，结果与文档需说明这一边界。

成功返回 writtenPath、sourcePath、savedRevision、当前 revision、源文件未保存状态和更新后的文件标识。必须区分磁盘替换是否完成和后续版本/缓存维护是否完成：若文件已写入但后续维护失败，返回 diskCommitted=true 及告警，不能将其描述为“未保存”并盲目重试。

### 另存为：只生成副本

用户已确认：`pvf_archive_save_as` 将当前内容写入指定目标，sourcePath 保持不变，后续 save 仍写原文件，workspaceId 不因副本生成而变化。

生成副本不代表原文件已保存：保留相对源文件的未保存状态、待同步变更和自动备份，不将当前版本标记为已写回源文件。若当前内容本来就与源文件一致，仍保持无未保存变更。返回 writtenPath、sourcePath、snapshotRevision、sourceDirty，避免客户端将副本路径误认为当前源路径。

当前 SaveAsDialog 会清理 mutations/自动备份并重置前端 dirty 基线，不能原样复用为“生成副本”的完整语义。应提取不弹对话框的 SaveCopy(path, options)，隔离序列化过程对工作区的副作用；UI 另存为也复用该门面并保留源文件未保存状态。

目标路径必须是绝对路径，父目录须已存在；目标与源文件等价时返回 SAME_AS_SOURCE，要求调用 save。按规范化路径、符号链接和文件身份识别同一文件，覆盖目标不允许是目录。

默认 overwrite=false，目标已存在返回 TARGET_EXISTS；显式 overwrite=true 时还需提供此前观察到的目标文件标识，并在提交前复核，目标变化返回 TARGET_CHANGED。冲突结果可返回非敏感文件标识供下一次明确覆盖请求使用。不得以“先判断不存在再普通 rename”实现禁止覆盖，须使用平台支持的原子禁止覆盖提交方式。

### 前端同步与任务边界

后端增加统一生命周期门面，UI 打开/关闭/保存/另存为与 MCP 共用互斥和状态检查。原 ArchiveService.Close 会直接丢弃修改，必须由门面保护；不能直接暴露为工具。

前端所有归档编辑视图上报草稿状态与代次；生命周期操作通过带 operationId 的 prepare/ack 检查各窗口并短暂冻结编辑，收到全部有效响应后复核 revision，再进入提交。超时或窗口状态未知返回 UI_NOT_READY，不能按“无草稿”继续。纯前端 dirty 布尔值的延迟上报不足以消除保存竞争。

打开、保存和另存为可能耗时，使用 taskId 与 task_status 返回阶段进度，避免长时间阻塞单次 MCP 调用。首版不开放生命周期任务取消：文件替换开始后不能假定可撤销；退出/停用服务等待不可中断提交完成，任务始终报告最终磁盘结果。

沿用 archive:opened/closed/saved，并增加副本生成事件 archive:copy-saved。事件携带 workspaceId、revision、来源、operationId 和 writtenPath；副本事件不能触发清除原文件 dirty 的逻辑。UI 可在重连时查询后端状态，事件不作为唯一事实来源。

## 导入、导出与 MOD

### 普通文件与目录导入

sourcePaths 使用本机绝对路径，targetDir 使用归档内部路径，空串表示归档根。单文件按文件名映射，目录保留被选目录名称和内部层级，沿用现有 collectImportFiles 行为。例如选择 `/tmp/items/equipment`，导入到根后得到 `equipment/...`。多个来源映射到同一路径时拒绝并列明冲突，不依赖遍历顺序决定覆盖。

mode 支持现有 text 与 raw：text 按现有 UTF-8 校验及归档文本编码规则写入，raw 按原始数据导入；预览报告实际类型。拒绝符号链接来源、特殊文件及非法归档路径；目录遍历设置文件数量、单文件与总字节数上限，沿用项目对 secrets 的禁止读取要求。

prepare 只生成隔离预览，展示新增/覆盖、目标路径、文本差异或二进制大小/摘要。现有 PreviewImport 不保留内容，ImportFiles 会重新读取磁盘；需提取保留冻结字节和来源摘要的导入计划，apply 消费相同快照，不能重新读取变化后的来源。大快照可使用受控临时存储，按 TTL、丢弃、归档切换及应用退出清理；分页替代现有最多 500 条后截断的预览形式。

apply 明确传入 selectedKeys；覆盖已有条目还须在 overwriteKeys 中逐项指定，默认不覆盖。先检查全部目标与依赖，再通过隔离 stage 一次提交所选内容；失败不部分写入归档。提交后的索引或版本维护错误需区分 workspaceCommitted，避免重复导入。成功后更新 revision、树、编辑器与版本状态，源 PVF 保持未保存。

导入不自动删除来源中缺失的归档文件，也不自动注册 `.lst` 或重写字符串表关系；关联修改需显式导入关联文件或使用脚本处理。工具说明需明确文本与 raw 模式的区别。

导入应用与脚本应用共用写入门面、revision 检查和前端草稿保护。已有运行任务或待处理写入计划时拒绝开始新导入；导入计划与脚本计划均属于待处理写入计划，open/close/save/save_as 的 PLAN_PENDING 检查须同时覆盖二者。

### 统一导出与来源

普通导出和导出为 MOD 共享 prepare/preview/execute，通过 mode=direct 或 mod 区分；MOD 格式通过 export_formats 发现。首版来源包含 selection（指定文件/目录，文件集由调用方展开）、memory（未启用版本控制的内存改动）、working（相对 HEAD 的工作区变更）和 commit（指定提交）。来源附加参数包括 commitID；首版不暴露 reverse 或 archive-diff 来源。

direct 沿用现有文本/二进制导出规则，不宣称所有类型都能无损往返；无法表达的类型须报告，不能静默导出空文件。全量导出可使用 selection 的归档根 scope，仍受文件数量与总字节上限约束。现有 archive-diff 来源在 AI 能发现/创建比较结果后再接入，首版不要求提供无法发现的 diffID。

导出读取已提交的后端内容及 Overlay，不应用前端草稿、脚本/导入预览，不创建提交，也不保存 PVF。存在归档文本或 GUI 草稿时返回 UI_DRAFT_PENDING。快照记录 workspaceId、revision、版本会话、HEAD 和来源身份；执行时失效返回 STALE_EXPORT，要求重新准备。

includeDependencies 必须显式声明。预览返回依赖数量、缺失/跳过原因并支持完整分页。selectedEntryKeys 对应预览中的输出文件路径，由服务映射到逻辑内容项，MOD 元数据由服务重建，不能作为普通内容排除。复用现有从冻结快照排除内容的语义，不重新计算或自动补回依赖；最终选择若移除依赖，服务返回依赖警告，执行须显式匹配 acknowledgedWarningIds。

### MOD 格式边界

首版复用 internal/mod 注册的 110USextend Writer，输出为 `<outputParent>/<name>/`，含 pack.json 和所需的 pvf/、merge/，不是压缩成单个 .mod 文件。

name、version 复用现有校验，版本省略或留空默认 1.0。普通脚本放入 pvf/；`.lst`、`.str` 按格式写入 merge/。工作区/提交来源有准确基线时输出新增或修改的表项；无基线的整表按现有格式规则跳过并返回警告，不回退为整表覆盖。

该格式不支持文件和表项删除，返回 skippedDeletes 与明细；纯删除或筛选后无可应用内容时拒绝输出。direct 的删除项输出修改前内容，这属于文件导出语义，不表示生成删除补丁。不能把导出成功描述为支持全部变更或已验证游戏效果。

跳过、解析失败、依赖不完整项赋予稳定 warningId，execute 需显式回传已查看的 ID；非法路径和无法表达的内容仍硬失败。首版不实现 MOD Reader，也不把 MOD 目录的 pack.json、merge/ 等包装内容当成普通归档资源自动导入。

### 发布、计划与任务

outputParent 使用本机绝对路径。首版 direct 显式指定 outputName，在父目录中新建专属输出目录；MOD 使用 name 作为目录名。同名目录存在一律返回 TARGET_EXISTS，不合并或覆盖。两种模式均先写入同父目录下的临时目录，再原子地以禁止覆盖方式发布，失败只清理本次临时目录。需调整现有 writeDirectDirectory 直接逐文件写入目标的入口，避免半成品和覆盖旧文件。

复用安全路径校验，检查符号链接、大小写及平台文件名冲突。结果返回 outputPath、文件数、依赖数、来源 revision、格式和警告；错误区分是否已经发布。prepare/apply/execute 按 requestId 去重，发布后丢失响应的重试返回同一结果，不能重复生成目录或误报目录已存在。

当前 ExportService 仅保留一份短期预览，新 Prepare 会替换旧预览。首版保持数量限制，但纳入共享占用与来源身份；UI 与 MCP 不得相互替换导出预览，已有计划返回 EXPORT_PLAN_PENDING。导入、导出和脚本计划使用不同 ID 命名空间，共用 task_status、TTL；本机连接共享计划。

导出预览存在期间允许编辑，但编辑使计划过期；open/close 需先显式释放导出计划。执行任务期间阻止切换/关闭及冲突写入，提交/发布阶段不开放取消。UI 显示 AI 来源、进度和最终目录，导入后刷新受影响标签页及目录树。退出/停用时等待不可中断提交，清理其余预览与临时存储。

## 现有实现需要调整的边界

### 单运行与单预览

当前 ScriptService 按实例互斥运行，core 只保存一个 scriptPlan；Run 会丢弃旧预览。首版保持单任务、单预览，新增统一的占用状态，覆盖 UI 和 MCP 两个入口：已有待处理预览时返回 PLAN_PENDING，要求先应用或丢弃，不能静默替换。

预览记录来源与调用方身份。本机 AI 连接共享任务和预览，UI 可以查看、应用或丢弃 AI 预览；若用户在 UI 处理了计划，AI 后续查询得到对应终态。占用检查和任务接纳必须原子执行，不能只在 MCP handler 中检查一次。

### 工作区一致性

继续使用现有 archive 身份和 batchRevision 校验，并增加对外不透明 workspaceId；切换或重新打开归档必须更换身份。传入路径在当前身份下解析，避免长期暴露会因增删文件而改变的 index。

界面和 AI 均可能编辑工作区。执行开始、运行完成和应用提交分别校验身份与 revision，冲突返回 STALE_WORKSPACE 或 STALE_PLAN，不覆盖最新编辑。多文件应用继续通过现有事务和依赖校验完成，不在 MCP 层循环调用 SetText。

### 应用副作用

归档修改应用到 Overlay，沿用版本记录与索引失效逻辑。字符串表关联修改必须一起选中或由服务报告缺失依赖，不能静默部分成功。

现有文件集脚本会在 Apply 时写入用户配置。首版 MCP 明确拒绝包含文件集变更的计划，返回 UNSUPPORTED_CHANGE，避免将其描述为“仅修改未保存的 PVF”。未来开放时应提供独立能力与副作用说明。

### UI 同步

目前运行结果和 planId 主要保存在前端 Pinia 中，仅复用后端日志事件不足以让界面接管 MCP 运行。新增后端任务状态查询，以及 started/completed/preview-created/applied/discarded 状态事件，事件携带 taskId、来源和 workspaceId。

前端打开脚本工作区时先拉取后端状态，事件用于后续增量刷新；AI 生成的源码作为独立任务展示，不能覆盖用户当前未保存的源码。日志按游标和字节数截断，明确返回 hasMore/truncated。

### 超时、重试与释放

沿用现有脚本源码 1 MiB、执行 5 分钟和日志 5000 条限制，再为日志单条大小、总字节、响应大小、预览寿命和任务历史设置上限。Goja 的 context 中断不是内存硬隔离，应限制并行数并验证大分配场景；需要严格内存上限时再考虑执行子进程。

run/apply、open/close/save/save_as，以及导入导出的 prepare/apply/execute 使用调用方范围内的 requestId 去重。同一 ID 和相同参数返回已有结果，相同 ID 配不同参数返回冲突；保存请求摘要及有限期结果，避免网络重试重复应用、保存或发布。去重记录跨工作区切换保留，重试先查去重记录再验证旧工作区身份。应用重启后无法确定的提交结果要求核对磁盘文件，不直接重放。

取消按 MCP 所属 taskId 校验；MCP 请求返回后后台脚本任务仍运行，显式 script_cancel 工具负责停止。MCP open/close 先要求处理已有脚本任务和预览；应用退出和服务停用时取消可取消任务、等待不可中断生命周期提交并释放预览。连接中断不立即丢弃预览，按可配置的有限 TTL 回收。

## 本地访问控制

只绑定 loopback，校验 Host 和 Origin。首版不做鉴权，连接配置只提供服务 URL；本机客户端共享 MCP 任务和计划。关闭服务时停止接受请求。

不把 EditorService 或整个 Wails API 自动注册为 MCP 工具。所有能力使用明确工具列表和参数 schema；返回业务错误码、简短描述和可执行的恢复建议。

## 实施顺序与验收

1. 服务层增加共享任务管理、预览占用和调用方身份，提取归档生命周期门面、SaveCopy、冻结导入计划及无对话框导出接口；保持现有 UI 可用。
2. 加入 MCP Server 和工具，复用现有服务；补充身份、revision、workspaceEpoch、分页、去重与资源释放逻辑。
3. 在现有设置末尾增加 MCP 配置区，并接通 UI 状态查询与事件。
4. 接通各窗口草稿 prepare/ack 与生命周期事件，验证真实 MCP 客户端的发现、打开、搜索、读取、执行、预览、应用、导入、保存/生成副本、普通导出、MOD 导出及关闭全链路。

针对性测试覆盖：UI/MCP 并发互斥、待处理预览保护、归档切换、编辑冲突、调用方隔离、重复请求、取消与超时、过期回收、字符串表依赖、文件集变更拒绝、分页截断、Host/Origin 拒绝。生命周期另覆盖：空工作区打开竞争、打开失败保留原状态、dirtyPolicy 三种分支、各窗口草稿与响应超时、外部源文件修改、目标覆盖竞争、路径别名、保存失败重试、磁盘成功后维护失败、生成副本保留源路径/dirty/自动备份、切换后的请求去重及旧任务结果隔离。运行 `go test ./...` 和前端构建；真实 PVF 回归按项目惯例通过 PVF_TESTFILE 启用。

导入导出另覆盖：来源文件在预览后变化、目录映射冲突、部分选择与覆盖许可、源路径与目标路径混用、快照限额与清理、提交后的维护失败、导出计划互斥和过期、文件筛选后的依赖警告、同名目录竞争、输出失败清理、发布后的重复请求，以及 MOD 无基线表、删除跳过、空包和最终元数据。复用现有 import/export/mod 测试，并运行前端 `npm test` 验证选择和预览交互。

## 官方参考

标注说明的后续只读扩展见 [MCP 标注文档查询协议](MCP标注文档协议.md)，设计了规则搜索、规则详情和实际文件解释三个工具。该扩展当前仅完成协议设计，尚未注册到 MCP 服务。

- MCP 传输规范：https://modelcontextprotocol.io/specification/2026-07-28/basic/transports
- 官方 Go SDK：https://github.com/modelcontextprotocol/go-sdk
- 项目现有 MOD 语义：[MOD.md](MOD.md)
