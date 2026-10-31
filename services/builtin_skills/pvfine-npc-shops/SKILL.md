---
name: pvfine-npc-shops
description: 通过 pvfine MCP 读取和修改 NPC 商店商品、分页、分类、金币价格与材料兑换成本。
---

# NPC 商店

先定位 NPC，再通过关联标注追踪实际 `.shp` 路径；必要时搜索 `**/*.shp`，用 NPC/商店列表确认 ID。不能猜测 NPC ID 就是商店文件名。

商店商品位于 `sell info/tab/item list` 或 `sell info/tab/category entry/item list`。tab 可重复，category entry 的 id 标识分类；修改前明确目标分页与分类。`[use category]` 关联的分类定义来自 `etc/itemshop/itemshopcategory.etc`，结构是 itemshop category/category name 与 category entry/id、text。未要求时不改共享分类定义。

item list 是商品 ID 列表，不是 ID/数量配对。读取实际 section 及标注后追加/移除目标商品，保留商品顺序与其他分页；新增前验证物品存在，并说明重复商品如何处理。不要把加入商店理解成创建新的物品文件。

金币和材料成本在商品自身文件（通常 .equ/.stk），不是商店列表：

- `[price]` 单个非负整数。删除 price 与显式设 0 含义不同。
- `[need material]` 以 `材料ID 数量` 成对存储，不能只改前半对或把多个材料改成单值。

用标注关联或物品 lst 联合解析商品，不把 ID 当文件索引。修改物品成本会影响所有引用该物品的商店；用户只想修改单个 NPC 价格时必须说明共享成本的影响，先确认所需效果，不能默改全局商品成本或自行复制物品。

把商店列表与物品成本的相关变更放在同一个脚本预览；涉及名称引用时一并包含 .str 依赖。对同名 tab、同一分类的多个 entry 要显式定位 occurrence，不选第一个充数。应用后同时复查商店列表、商品成本和关联是否正确。

操作前阅读 [MCP 操作流程](references/mcp-workflow.md)，按其查询、验证、预览、应用和保存规则执行。
