# 文件 GUI 模式

GUI 模式与侧边文件预览独立。文件默认显示文本，匹配 GUI provider 后才显示模式切换。当前注册 `.shp` 商店，支持商品及分页编辑。

## 扩展入口

- `frontend/src/gui/registry.ts` 注册 provider：唯一 `id`、显示名称、`readOnly` 能力、文件匹配条件及异步 Vue 组件。
- provider 组件接收 `file`（文件索引、路径、当前草稿文本、可编辑标记）和 `active`。通过 `FileGUIHost` 装载；隐藏时保留组件状态，关闭文件时销毁。
- 显示模式归属窗格，每个窗格独立保存文件模式。文本编辑器使用隐藏而非卸载保留光标、滚动和撤销历史。
- `useFileGUIStore` 提供归档 epoch 与内容 revision。异步读取必须检查请求代次，防止关闭、切换归档或重复请求的旧结果覆盖新内容。

## 商店数据

`FileGUIService.ReadShop(fileIndex, text)` 只读取数据：商店结构来自调用者传入的当前草稿；商品、材料和职业引用来自当前归档 overlay。展示使用 overlay；编辑表单通过 `ReadItem(id, drafts)` 优先读取关联商品的未保存草稿。

`ShopDocument` 保留 Tab、分类区块及商品出现顺序，不合并重复商品。区块和商品记录包含 UTF-16 源位置，用于精确修改或删除某一次商品出现。

商品成本来自商品文件：`[price]` 为金币，`[need material]` 每两个值组成道具 ID 和数量。两类成本共存；缺失成本与显式零金币不同。未知引用和异常结构返回数据提示，缺图使用占位。

`[use category]` 的 `expert job` 与 `expert job non filter` 使用相同的副职业枚举：0 炼金术师、1 附魔师、2 控偶师、3 分解师。展示保留原始分类类型。

职业关系与搜索索引均使用角色文件 `[growtype name]` 的第一个值，缺失时回退 `[name]`。此规则位于共享元数据提取及关联名称解析中；语义缓存版本变化会使已有内存持久化缓存和 SQLite 缓存失效。

## 编辑与检索

- 商品 hover 或键盘聚焦后显示编辑、删除按钮。更换商品 ID 会先读取新商品的成本，再允许修改；删除只移除当前出现，不删除物品文件。
- 底部入口为添加商品、分页管理、批量设置。分页管理支持新增、重命名及删除，删除分页会同时移除其中全部分类的引用。
- 批量设置覆盖当前分页的全部大分类，按关联物品文件去重。金币和兑换材料可分别选择保持或替换；空金币会移除 `[price]`，空材料列表会移除 `[need material]`。
- 价格是物品文件的共享属性，修改会影响所有引用它的商店；界面明确提示此行为。
- `ApplyShopEdit` 校验归档 revision、文件路径与定位，再在独立 staging 中构造所有受影响文件；全部校验完成后一次性提交 overlay，并记录一次版本撤销操作。原 PVF 不会自动写盘。
- 前端携带未保存草稿，后端只使用实际受影响的文件；前端提交期间锁定文本输入，成功后同步相关标签。过期表单会被拒绝，不覆盖更新后的内容。
- `ItemPicker.vue` 是通用装备/道具选择器，支持图标、ID/名称/路径检索、延迟搜索、分页及过期请求隔离。`ArchiveService.SearchItems` 在内存和 SQLite 索引中都先限定物品范围再分页。

## 加载状态与验证

首次加载使用两列骨架屏，刷新保留上次内容并暂时禁用分类和 Tab；失败显示重试入口，明确标记旧数据。图标独立加载和淡入，遵守系统减少动态效果设置。

验证命令：

```sh
go test ./...
PVF_TESTFILE=/path/to/90CN.pvf go test ./services -run 'TestShopReal90CN|TestShopEditReal90CNRoundTrip' -count=1
cd frontend
npm test
npm run build
```

真实样例为 `itemshop/101_joann_box.shp` 和 `itemshop/equipmentshop1.shp`。图标需要在应用中配置可用的 NPK 资源目录。
