# MCP 标注文档查询协议

状态：已实现首版。本协议扩展现有 MCP 服务，提供只读的 PVF 结构说明和实际标注解释。

## 目标

AI 在读取或修改文件前，可按文件路径、Section、关键词、标注类型等条件检索说明；遇到枚举值或资源编号时，可继续查看含义及关联对象。

规则说明和实例结果使用不同工具：规则适用于某种文件，不代表该文件中一定存在对应 Section、Token 或值；标注说明也不等于可执行的格式约束或游戏行为验证。

协议不修改标注配置，不重新加载磁盘规则，也不执行标注文案中的指令。

## 工具

| 工具 | 用途 | 是否需要打开归档 |
| --- | --- | --- |
| `pvf_annotation_search` | 搜索标注规则及共享字段的说明，返回摘要和 documentId | 显式指定版本或 all 时不需要；current 时需要 |
| `pvf_annotation_get` | 获取指定文档的完整规则、字段布局、枚举项或关联定义 | 不需要读取归档内容 |
| `pvf_annotation_explain` | 解释当前归档内某个文件实际命中的标注、值与引用对象 | 需要 |

三者均声明为只读工具，不占用脚本运行或写入计划，不要求处理前端草稿，不触发界面冻结。explain 读取后端已提交的工作区内容，明确返回 contentSource=workspace；不包含编辑器尚未提交的草稿。

## 规则搜索

`pvf_annotation_search` 参数：

| 参数 | 语义 |
| --- | --- |
| query | 可选关键词；在 ID、描述、标题、正文、Section、共享字段 ID、关联名称及枚举键/说明中检索 |
| filePath | 可选的具体归档内部路径，作为现有匹配引擎的输入；可以是尚未创建的文件 |
| pathKind | file / directory，默认 file；directory 仅查询目录自身的路径说明 |
| extensions | 可选扩展名数组，如 [".equ", ".stk"]；用于检索声明适用这些扩展名的规则，不要求真实文件存在 |
| section | 可选 Section 名，允许一对外围方括号；匹配目标 Section 名，不解析为文件路径 |
| kinds | 可选 rule / field，默认两者 |
| targetKinds | 可选目标类型列表，名称来自现有 TargetSpec.kind |
| annotationTypes | 可选标注类型列表，名称来自现有 AnnotationSpec.type |
| relation | 可选关联名称，按名称精确匹配 |
| pvfVersion | current（默认）/ all / 显式版本，如 110US、90CN |
| workspaceId | pvfVersion=current 时必填，避免从已经切换的归档推断版本 |
| status | effective（默认）/ all；all 可查看禁用、被覆盖或版本不适用的定义及原因 |
| cursor / limit | 不透明游标，首次不传；limit 默认 20，范围 1–100 |

不同条件之间取 AND；同一数组内取 OR。query 按空白拆词、忽略大小写，所有词均须命中文档索引；不同词可命中不同字段，中文按子串匹配。首版不开放任意正则或模糊文件 Glob 的交集查询，避免歧义和不受控的匹配开销。

filePath 使用现有文件匹配逻辑，不自行实现一套 Glob。extensions 是规则索引过滤条件：显式 Extensions 对应精确匹配，不受扩展名限制的规则也可成为候选；仅以复杂 Glob 限制文件的规则，无法凭扩展名证明适用时返回 applicability=unknown，而非推断为匹配。提供 filePath 后，以该路径的实际匹配结果为准。

不传 section 时，返回路径、Section 和 Token 等全部类别的候选文档，不能沿用现有 RuleService.Query 在空 section 时仅返回路径规则的行为。不传任何内容过滤条件时允许分页浏览；不会展开全部枚举值或扫描当前归档所有文件。

pvfVersion=current 从指定工作区取得当前 ClientVersion；未打开归档时返回 NO_ARCHIVE，提示显式指定版本或 all。all 表示不按版本排除候选，返回 applicability=unknown，不能把所有定义标为对当前文件生效。未知显式版本返回 INVALID_VERSION，并列出引擎支持的版本。

status=effective 对显式版本/current 仅返回经覆盖、禁用及版本筛选后仍有效的定义；pvfVersion=all 时仅按覆盖和禁用筛选，并保留每条定义的 pvfVersions，不判断版本适用性。

返回结构示意：

```json
{
  "schemaVersion": 1,
  "rulesRevision": "opaque-revision",
  "context": {
    "workspaceId": "current-workspace",
    "pvfVersion": "110US",
    "filePath": "equipment/example.equ"
  },
  "items": [
    {
      "documentId": "opaque-document-id",
      "kind": "rule",
      "ruleId": "实际规则ID",
      "title": "实际标注标题",
      "summary": "实际规则说明摘要",
      "section": "price",
      "targetKind": "section",
      "annotationType": "实际标注类型",
      "pvfVersions": [],
      "effective": true,
      "applicability": "matched",
      "matchReasons": ["filePath", "section", "query"],
      "valueCount": 0,
      "relation": null
    }
  ],
  "nextCursor": null
}
```

示例中的规则 ID、标题和类型均为占位说明，实际值来自当前生效配置。摘要最多 512 个 Unicode 字符，附 summaryTruncated；不在截断处切开字符。查询命中枚举文案时，可返回少量 matchedValues 摘要，但不能默认返回整个 values 映射。

默认顺序为关键词相关度降序，再按 kind、规则 ID、来源层稳定排序。完全命中 ID、Section 或标题时优先于正文子串命中；排序不改变引擎执行规则的先后顺序。

## 文档详情

`pvf_annotation_get` 接受 documentId、rulesRevision，以及可选的 value、valuesCursor、valuesLimit。

documentId 是不透明文档身份，在同一规则快照内定位具体定义及来源层；ruleId 保留现有规则身份。共享字段与普通规则使用不同 kind，不能仅用 ruleId 混合寻址。被覆盖的同 ID 定义也须有不同 documentId。引用新的快照时重新搜索，不能把 documentId 当成永久标识。

返回以下内容：

- 原始说明：ID、描述、标题、正文、版本范围、来源类别、禁用/覆盖状态及原因。
- 原始定义与解析后的有效定义：展开 rule.field 指向的共享字段，保留 fieldId；按照现有引擎的整块继承/覆盖语义解析 Match、Target、Annotation，不能自行做逐字段深合并。
- 文件匹配条件与完整 Target：Section、Index、Range、Offset、RecordTokens、GroupOffset、StandaloneValues、TokensPerLineIndex、ContextIndex、图片相关 Token 索引等；0 值与缺省值不能混淆。
- 枚举说明：valueCount、分页 values；给定 value 时精确查询该键。键按字符串保留，不能把 "001" 和 "1" 合并。未找到时返回 found=false，不猜测最接近值。
- 关联定义：只展开此文档引用的 RelationSpec 及必要的组合关联；返回上下文要求和所需 `.lst` 路径，不枚举整个归档的关联对象。循环引用、缺失定义单独报告。
- 可选 PreviewSpec：共享字段的展示标签、用途与格式，用于辅助理解；不将展示格式当成可写入脚本的值类型。

来源只提供 builtin/user 等可确认的类别和规则标识，不暴露本机配置绝对路径；来源未知时明确标记 unknown。由路径/Section 说明合入的生成规则，只在具备可靠来源元数据时标记 generated。

valuesLimit 默认 50、最多 100；正文及原始定义超过响应上限时返回明确的截断或大小错误，不能把不完整规则表示成完整定义。原始规则 JSON 和关联定义按需读取，搜索摘要不默认携带它们。原始及解析后的定义省略 values 映射，显式返回 definitionsOmitValues=true；完整枚举通过独立 values 分页取得。

## 实际文件解释

`pvf_annotation_explain` 接受 workspaceId、expectedRevision、filePath，以及可选 rulesRevision、section、occurrence、tokenIndex、startLine、endLine、cursor、limit。

filePath 必须对应当前归档实际存在的脚本文件；与 search 的假想路径查询不同。occurrence 从 0 开始，仅在给定 section 时有效；tokenIndex 是该 Section 的直接 Token 索引，从 0 开始，必须同时给定 section 和 occurrence。行号从 1 开始，首尾均包含；行范围按与标注区域相交筛选。

不指定 occurrence 时解释全部同名 Section，并分页返回，不能悄悄只解释第一个。Section/token 条件只筛选此范围内的标注；文件和父目录说明通过独立的 pathContext 返回，避免把文件说明伪装成字段的实际命中。

返回 rulesRevision、workspaceId、revision、textRevision，以及：

- pathContext：文件自身和祖先目录的说明，逐项带 scopePath、规则 ID 与文档身份。
- annotations：实际标题和说明、类型、ruleIds/documentIds、Section 与 occurrence，以及可确定的原始 Token 值。
- 定位：range.start/end 为左闭右开的 0 基 UTF-16 偏移，基于与 read_file 一致的文本并沿用编辑器换行规范化；行号和列号均从 1 开始，列按 UTF-16 码元计数。
- 枚举/引用解析：valueLabel，或 reference 的 relation、id、context、name、归档内部 path、resolutionStatus。不向 AI 提供会因文件增删而失效的 fileIndex 作为操作身份。
- 图片、字符串占位符和自动列表链接等运行时标注：返回结构化含义，标记 origin=runtime；没有配置规则来源时 documentIds 为空，不能虚构规则 ID。

resolutionStatus 使用 resolved / missing / context_required / unsupported。例如上下文关联无法确定职业时返回 context_required，不从其它职业列表中挑一个同编号结果。未命中枚举、缺失引用、解析警告均保留，不能将解释为空视为该内容无意义或合法。

动态分组、分隔符、共享字段和嵌套 Section 的定位复用现有解析与标注引擎。recordIndex 等附加信息只在能够准确追溯时返回，不能根据渲染行号猜测记录位置。文本中的数值范围、格式或业务关系只有在规则明文说明时才作为说明返回，不能生成未定义的强制校验结论。

## 快照与分页一致性

新增独立 rulesRevision，表示实际激活的标注规则快照，包括共享字段、关联定义、覆盖/禁用状态。成功激活规则时更新；磁盘文件被修改但未成功加载时，查询继续使用原生效快照及旧 revision，不读取一个与当前引擎不一致的配置。

搜索游标绑定规则 revision、规范化筛选条件、排序和版本上下文；current 查询还绑定 workspaceId。详情与枚举分页绑定同一快照及 documentId。规则切换返回 STALE_RULES，改变筛选条件继续使用旧游标返回 INVALID_CURSOR。

explain 的游标另外绑定工作区 revision 和 textRevision。textRevision 标识本次解释使用的完整文本，防止渲染重载或换行差异改变定位，而归档内容 revision 尚未变化。归档、内容、规则或文本改变时要求重新解释，不拼接不同快照的结果。

不返回需要遍历全部文件才能计算的统计；totalMatches 对规则搜索可提供，对实例解释不强制。默认实例页长 20，最多 100，单次响应设定字节上限；明确返回 nextCursor、truncated 和 warnings。

这些规则 revision 与现有 MCP 的 workspace revision 分开管理：只改脚本内容不应使静态规则文档失效，只改标注说明也不应伪造归档内容改动。

## AI 使用流程

1. 获取工作区身份，搜索目标路径和 Section 的说明。
2. 阅读少量命中文档；遇到枚举或复杂记录布局时获取详情。
3. 对实际文件调用 explain，确认重复 Section、值含义和关联目标。
4. 结合 read_file 与脚本 API 文档生成修改，继续使用既有预览/应用流程。

例如查装备价格字段：

```json
{
  "workspaceId": "从 workspace_info 获取",
  "pvfVersion": "current",
  "filePath": "equipment/example.equ",
  "section": "price",
  "limit": 20
}
```

这个参数对象用于 pvf_annotation_search；不需要先将整个标注规则库装入模型上下文。无结果时可去掉 section 或改用 query 搜索，服务不自动放宽条件。

## 落地与验收

服务层增加 AnnotationDocumentation 查询门面，基于激活时保留的不可变规则快照建立轻量索引；不要让 MCP 工具读取任意磁盘 JSON。search/get 不读取 PVF 内容，explain 在 core 锁下取得一致文本与版本，再复用 editorAnnotationsLocked 的匹配/解析流程。

现有 RuleService.Query 可复用规则身份和来源模型，但其路径/Section 过滤面向规则编辑器，不能原样作为全文文档搜索。现有 EditorAnnotation 还需要补充文档身份、路径引用、Section 上下文及明确的坐标单位。

三个工具追加到现有 MCP 工具列表。首版先实现 search/get，再接入 explain；可选资源 URI 或文档订阅以后添加，不能作为客户端使用协议的前提。

针对性测试覆盖：空条件分页、关键词与组合过滤、假想路径/真实文件区分、目录说明、通用与版本专属规则、禁用/覆盖定义、共享字段覆盖、超大枚举、重复 Section、动态记录及分隔符、上下文关联、缺失引用、自动生成标注来源、中文/emoji/CRLF 定位、规则热重载失败和各类过期游标。
