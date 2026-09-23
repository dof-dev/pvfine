---
name: pvf-json-rules
description: >-
  在 pvfine 项目中直接编辑 config/annotations.json 和 config/rendering.json，
  新增或调整 PVF 标注与脚本渲染规则。涵盖文件/Section 匹配、重复记录、-1
  分隔、groupOffset、动态宽度、嵌套 Section、共享字段、关联列表、验证与重新加载。用户要求
  “加标注”“调整渲染”“修改 JSON 规则”“为新 section 配置格式”时使用；
  不通过标注规则编辑器。
---

# 直接编辑 PVF 标注与渲染 JSON

目标：基于实际 PVF token/Section 结构修改**仓库中的源配置**，以最小改动新增规则；不使用 `tools/annotation-editor.html` 或编辑器 HTTP API。

## 1. 定位规则并确认数据

- 标注：`config/annotations.json`（`version: 1`，含 `fields` 和 `rules`）；关联 ID 的定义在**另一个文件** `config/lists.json` 的 `relations` 内。不要仅因标注需要关联 ID 就往 `annotations.json` 新增 `relations`。
- 渲染：`config/rendering.json`（`version: 1`，含 `rules`）。
- 从实际反编译文本确定文件相对路径、扩展名、Section 名称（JSON 中不带方括号）、token 顺序、类型、前置元数据、分隔值和完整记录宽度。不要从换行推断原始 token 边界；显示用的换行与标注记录宽度是两套独立配置。
- 编辑前搜索是否已有相同 Section、相同路径/扩展名的规则；能改现有规则就不创建冲突的重复规则。对 `config/*.bak` 不做手工同步；保留既有修改，避免覆写整份大型 JSON。

## 2. 标注规则：`config/annotations.json`

在顶层 `rules` 数组增加一个对象。示例是**一条规则**（不是整份 JSON）；插入时保持现有 `version`、`fields`、`rules` 等属性不变：

```json
{
  "id": "example.world-drop",
  "description": "按组标注掉落物品",
  "match": {"glob": "etc/example.etc"},
  "target": {
    "kind": "token",
    "section": "world drop",
    "index": 0,
    "recordTokens": 2,
    "offset": 0,
    "groupOffset": 2,
    "standaloneValues": [-1]
  },
  "annotation": {
    "title": "物品",
    "type": "reference",
    "relation": "物品"
  },
  "group": "etc"
}
```

上例的 `relation` 必须已经存在于 `config/lists.json`（若本仓库重命名/删除，应改用实际存在的 relation）。`match: {}` 表示不限制文件；`match.extensions: [".etc"]` 按扩展名限制；`match.glob: "etc/**/*.etc"` 按归档内相对路径限制，两者同时存在时都必须匹配。glob 的 `**` 可跨目录。规则 ID 在 `rules` 内必须唯一；`pvfVersions` 可选，只允许 `90US`、`90CN`、`110US`；`group` 只决定编辑器中的显示分类，不代表 token 分组。

### target 选择与索引

| 字段 | 用途 |
|---|---|
| `kind: "path"` | 对文件/目录路径打 `text` 标注。 |
| `kind: "section"`, `section` | 对该名称的 Section 标签打 `text` 标注。 |
| `kind: "token"`, `section`, `index` | 对 Section 中的直接 token 打标注；非重复模式下 `index` 是 Section 内从 0 开始的绝对索引。 |
| `range: {"start":0,"endExclusive":2}` | 连续范围，只能用于 `text`；和 `index` 二选一。 |
| `recordTokens: 2` + `index: 0` | 开启重复记录：每个**完整** 2-token 记录的第 0 个 token 被标注。此时 `index` 是记录内部的下标，不是整个 Section 的下标。 |
| `offset: 2` | 仅跳过整个 Section 开头的 2 个直接 token，然后才开始查找分隔值及记录；只应用一次。 |
| `standaloneValues: [-1]` | 指定**整数**分隔值，命中后结束当前组，分隔值本身不标注；下组重新计算，不跨组拼接不完整记录。支持多个值。 |
| `groupOffset: 2` | 每个由分隔值划分的组（**包括第一个组**）各跳过开头 2 个 token，然后按 `recordTokens` 匹配。没有设置分隔值时，整个剩余 Section 就是一组。 |
| `tokensPerLineIndex: 0` | 可选：从 Section **直接 token** 第 0 个读取正整数，覆盖本 Section 的 `recordTokens`；不存在/无效时回退到配置的 `recordTokens`。即使设置动态宽度，仍须配置正数 `recordTokens` 作为回退。 |
| `contextIndex` | 重复记录中上下文 token 的**记录内**下标，常用于 `contextual` 关联。 |
| `imagePathToken` | `image` 类型使用，同一记录中 IMG 路径 token 的**记录内**下标；`index` 是图片的数字索引 token，二者不可相同。 |

**执行顺序**：Section 直接 token → 应用一次 `offset` → 用整数 `standaloneValues` 分组（分隔 token 不入组）→ 每组应用 `groupOffset` → 按固定/动态 `recordTokens` 划完整记录 → 用记录内 `index` 定位标注。连续 `-1` 会形成空组，不产生记录。反引号包围的字符串（例如下方代码中的 `-1` 字符串 token）**不是**整数分隔符；仅整数类型 token 命中。

**嵌套 Section**：父 Section 的标注只统计自己的直接 token；子 Section 的 token 单独统计，不占父级记录宽度。若未闭合的父 Section 后接有闭合标签的子 Section，并希望 `[/子段]` 后的 token 继续归属父段，须在渲染规则中为父段声明 `format.nestedSections`。例如 `[independent drop]` 的 17-token 记录，第 2 个 token 是怪物 ID、第 3 个是物品 ID：分别用 `section: "independent drop"`、`recordTokens: 17`、`index: 1`/`2` 配置两条 `reference` 规则，relation 为 `怪物`/`物品`。每条记录后的可选 `[list]...[/list]` 不会打断父级计数；`[list]` 的 2-token 排版由单独的渲染规则控制。重复记录的 `index` 从 0 开始。

例如 `offset: 1`、`standaloneValues: [-1]`、`groupOffset: 1`、`recordTokens: 2`、`index: 0`：

```text
[world drop]
999 10 100 200 300 -1 20 400 500 -1 30 600 700
```

先排除全局 `999`；每组再分别排除 `10`、`20`、`30`，完整记录命中 `100`、`400`、`600`。第一组尾部 `300` 不足一条完整记录，因此不标注。注意渲染的 `standaloneValues` 只控制换行，不会自动给标注添加此配置。

**校验约束**：`offset`、`groupOffset`、`recordTokens` 不得为负；`tokensPerLineIndex` 不得为负；设置分组/偏移/动态宽度须同时设置正数 `recordTokens` 和单个 `index`，不能与 `range` 混用。`index`、`contextIndex`、`imagePathToken`（使用时）必须在配置的 `recordTokens` 范围内。`standaloneValues` 为 32 位有符号整数 JSON 数组；非数字字符串不合法。

### annotation 的常用类型

- `{"title":"说明","type":"text","content":"可选提示"}`：普通说明；`title` 可为空（仅显示 Tooltip 等行为）。
- `{"title":"稀有度","type":"enum","values":{"3":"神器","4":"史诗"}}`：token 内容到显示名的映射；`values` 不可为空。
- `{"title":"物品","type":"reference","relation":"物品"}`：关联 ID；`relation` 必须存在于 `config/lists.json`，上下文关联还须检查记录宽度及上下文下标。
- `{"title":"文件","type":"path","pathRoot":"equipment/"}`：目标 token 内容作为归档路径，`pathRoot` 必须是归档内相对目录；和 `target.kind: "path"`（标注文件路径本身）不是一回事。
- `{"title":"图片","type":"image","inlineImage":true}`：要求正数 `recordTokens`、独立 `index` 与 `imagePathToken`；不允许 `contextIndex`。

### 共享字段 `fields`

复用跨规则/预览的字段时，在顶层 `fields` 添加含唯一 `id`、`match`、`target`、`annotation` 的对象；这些字段本身会形成标注。显式规则可用 `"field": "字段ID"` 引用：引用规则自己的非空 `match`、`target`、`annotation` 可整体覆盖所继承部分，不是逐属性合并。只有需要结构化预览时才增加 `preview`（`provider` 或 `providers`、非空 `role` / `group` / `format`、`order`，可选 `label`）。重复记录字段使用同一套 `offset` / `groupOffset` / `standaloneValues` 语义。未被显式规则引用/覆盖的共享字段会作为隐式标注规则参与匹配；同一位置多个显式规则的标注会合并 Tooltip，先遇到的规则决定初始标题，**不要想当然地认为后面的规则覆写前面的规则**。

## 3. 渲染规则：`config/rendering.json`

在顶层 `rules` 数组新增一条；下面是实际 `[world drop]` 的换行需求：

```json
{
  "id": "section.world-drop",
  "match": {},
  "target": {"kind": "section", "section": "world drop"},
  "format": {
    "tokensPerLine": 2,
    "standaloneValues": [-1]
  }
}
```

效果：普通 token 每两个一行；遇到**整数** `-1`，无论该行已有几个 token，先换行，`-1` 独占一行，后续恢复每两个一行。其他 Section 不受影响。字段说明：

| 字段 | 用途 |
|---|---|
| `target.kind: "file"` | 整个文件的排版规则；不可配 `target.section` 或 `format.tokensPerLineIndex`。 |
| `target.kind: "section"`, `section` | 指定 Section 内排版；按名称不区分大小写匹配。 |
| `match` | 和标注类似，支持 `extensions` 与 `glob`，`{}` 表示所有文件。 |
| `format.tokensPerLine` | **必填正整数**；普通内容每行多少个 token，也是动态宽度的回退值。 |
| `format.offset` | Section/文件开头前 N 个 token 各占一行，此后再开始 `tokensPerLine` 分组；不是标注用的 `groupOffset`。 |
| `format.tokensPerLineIndex` | **仅 Section**：取 Section 内从 0 开始的直接 token 中的正整数作为每行宽度，失败则回退到 `tokensPerLine`。 |
| `format.standaloneValues` | 32 位有符号整数数组；指定的整数 token 独占一行，并重置后续的行计数。 |
| `format.nestedSections` | **仅 Section**：允许这些“有闭合标签”的子 Section 嵌套在当前未闭合父 Section 内，而不是把父 Section 提前结束；名称不带 `[]`。子 Section 自身的每行 token 数仍由它自己的 Section 规则决定。 |

例如 `[independent drop]` 每 17 个 token 一组，并允许每组后可选一个 `[list]...[/list]`，其中 list 每 2 个 token 一组：父规则配置 `"tokensPerLine": 17, "nestedSections": ["list"]`，再为 `section: "list"` 配置 `"tokensPerLine": 2`。有 list 时进入子 Section 的 2-token 分组，`[/list]` 后继续父级 17-token 分组；没有 list 时父级直接继续下一组。

渲染规则**不支持** `groupOffset`，也不支持标注的 `recordTokens` / `index`。两份 JSON 的同名 `standaloneValues` 含义相关，但前者只换行、后者只划分标注记录；有双重需求时两份文件都配。

**冲突规则**：渲染引擎分别寻找文件级和 Section 级规则。相同 target 下，`match.extensions` 和 `match.glob` 各贡献 1 级 specificity；更具体者优先，**相同具体度时越靠后的规则优先**。Section 的排版宽度通常优先于文件级宽度。若为某一个文件覆盖某个 Section，设置 `glob`，并确认它在匹配范围和优先级上不会被其他规则覆盖。

## 4. 修改、验证、重载（每次必做）

1. 先保存旧规则的修改上下文（`git status --short`、`git diff -- config/annotations.json config/rendering.json`），只追加或修改目标对象；避免重排整个文件，也不要覆盖他人未提交内容。
2. 修改后检查 JSON 语法和结构，例如：
   ```bash
   python3 -m json.tool config/annotations.json >/dev/null
   python3 -m json.tool config/rendering.json >/dev/null
   git diff --check
   ```
3. 使用项目内**真实 Go 校验**（仅 `json.tool` 无法校验语义）。至少执行：
   ```bash
   go test ./internal/annotations ./internal/rendering ./internal/pvf
   go test ./services -run 'Annotation|Rendering' -count=1
   ```
   已有 `LoadDefault` 测试能检查嵌入配置；新增复杂规则时给 `internal/annotations/*_test.go` 或 `internal/pvf/script_render_test.go` 增加“输入 token → 预期标注/换行”的针对性测试，覆盖组首、连续分隔符、尾部不完整记录和不匹配文件。需要检查完整 Go 项目时运行 `go test ./...`；环境/既有无关测试失败要单独说明。
4. 本地开发从仓库运行时，服务优先寻找 `config/annotations.json`、`config/rendering.json`；打包后可能使用用户配置目录下的运行时副本。修改仓库源文件**不保证已运行的应用自动更新**。按实际启动环境重新打开应用，或调用应用已有的 `AnnotationService.ReloadRules` / `RenderingService.ReloadRules`（会校验并替换活动引擎，失败时保留旧引擎）。不要在没有用户授权时重启、发布或修改用户运行时副本。
5. 给出修改的文件与规则 ID、几个代表性 token 的预期结果、验证命令与结果，以及是否需要用户主动重新加载。不要只因 JSON 语法通过就声称应用的运行时规则已生效。

## 5. 参考实现（需要新字段时先核对）

- 标注模型与校验：`internal/annotations/model.go`、`internal/annotations/loader.go`；分组与重复记录：`internal/annotations/engine.go` 的 `repeatedTokenGroups` / `repeatedRecordTokens`；共享字段：`internal/annotations/fields.go`。嵌套段的编辑器视图由 `internal/pvf/script_view.go` 的 `ParseScriptViewWithNestedSections` 解析，`services/annotations.go` 按当前文件的渲染规则提供父子段关系。
- 渲染模型、匹配优先级与校验：`internal/rendering/model.go`、`internal/rendering/engine.go`、`internal/rendering/loader.go`；实际 token 输出：`internal/pvf/file.go`。
- 热重载：`services/annotation_service.go`、`services/rendering_service.go`；内置配置来源：`config/embed.go`。如果本 skill 与代码不一致，以**当前代码及有效测试**为准，并同步更新本 skill。
