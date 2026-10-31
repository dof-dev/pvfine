---
name: pvfine-quests
description: 通过 pvfine MCP 读取和修改 PVF 任务条件、NPC、等级限制、对话、固定奖励和自选奖励。
---

# 任务

搜索实际 `.qst` 路径，用名称、quest lst 与关联标注确认任务 ID。读取现有字段及字符串引用，再确定是条件数值调整、任务类型转换还是奖励修改。未知字段原样保留。

基础字段：name、grade、difficulty、npc index（发布 NPC）、complete npc index、job、grow type（-1 为不限）、level（下限/上限）。grade 常见字符串值 `[daily]`、`[normaly repeat]`、`[common unique]`、`[achievement]`、`[epic]`，保留原始拼写，写入的是字符串值而非新 section。

条件由 type（字符串）、sub type（整数）、int data（整数组）共同决定：

|条件 type|sub type|int data 一组参数|
|---|---:|---|
|[hunt monster]|-1|副本ID、难度、怪物ID、数量（可重复）|
|[clear map]|-1|地图ID|
|[seeking]|-1|物品ID、数量（可重复）|
|[meet npc]|-1|NPC ID|
|[condition under clear]|6|副本ID、难度|
|[condition under clear]|11|副本ID、难度、房间数量|
|[condition under clear]|0|副本ID、难度、时间（秒）|
|[condition under clear]|5|副本ID、难度、人数|

切换条件需同步这三个字段，并验证目标 ID，不能沿用旧参数宽度。未知组合先查当前标注。

奖励：reward type 决定 reward int data 布局。`[item]` 是物品ID/数量对（可重复）；`[title]` 是称号ID/数量；`[grow type]` 是转职ID；`[awakening type]` 是觉醒次数。`reward selection int data` 为自选奖励，读取实际标注确认布局，不把它替换成固定奖励。quest point 是 QP。

对话字段 depend message、condition message、solve message 以及 name 可能使用 `<表号::键名>` 引用；先解析译文，修改时保留引用机制并预览字符串表依赖。不要把占位引用当作名称全文覆盖。重复字段、特殊 token 或未知布局先逐项确认，保留闭合标签与原有类型。新建/删除任务还必须同步正确 lst；不在单纯调整奖励时重建整个任务。

预览应说明任务身份、前后条件参数、奖励与文本引用，再应用并重新读取验证。

操作前阅读 [MCP 操作流程](references/mcp-workflow.md)，按其查询、验证、预览、应用和保存规则执行。
