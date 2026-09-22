# 文件 GUI 模式

GUI 模式与侧边文件预览独立。文件默认显示文本，匹配 GUI provider 后才显示模式切换。本版仅注册 `.shp` 商店，并以只读方式运行。

## 扩展入口

- `frontend/src/gui/registry.ts` 注册 provider：唯一 `id`、显示名称、`readOnly` 能力、文件匹配条件及异步 Vue 组件。
- provider 组件接收 `file`（文件索引、路径、当前草稿文本、可编辑标记）和 `active`。通过 `FileGUIHost` 装载；隐藏时保留组件状态，关闭文件时销毁。
- 显示模式归属窗格，每个窗格独立保存文件模式。文本编辑器使用隐藏而非卸载保留光标、滚动和撤销历史。
- `useFileGUIStore` 提供归档 epoch 与内容 revision。异步读取必须检查请求代次，防止关闭、切换归档或重复请求的旧结果覆盖新内容。

## 商店数据

`FileGUIService.ReadShop(fileIndex, text)` 只读取数据：商店结构来自调用者传入的当前草稿；商品、材料和职业引用来自当前归档 overlay。首版不读取其他文件尚未提交的前端草稿。

`ShopDocument` 保留 Tab、分类区块及商品出现顺序，不合并重复商品。区块和商品记录包含 UTF-16 源位置，供后续字段编辑定位；本版没有写回 API。

商品成本来自商品文件：`[price]` 为金币，`[need material]` 每两个值组成道具 ID 和数量。两类成本共存；缺失成本与显式零金币不同。未知引用和异常结构返回数据提示，缺图使用占位。

`[use category]` 的 `expert job` 与 `expert job non filter` 使用相同的副职业枚举：0 炼金术师、1 附魔师、2 控偶师、3 分解师。展示保留原始分类类型。

职业关系与搜索索引均使用角色文件 `[growtype name]` 的第一个值，缺失时回退 `[name]`。此规则位于共享元数据提取及关联名称解析中；语义缓存版本变化会使已有内存持久化缓存和 SQLite 缓存失效。

## 加载状态与验证

首次加载使用两列骨架屏，刷新保留上次内容并暂时禁用分类和 Tab；失败显示重试入口，明确标记旧数据。图标独立加载和淡入，遵守系统减少动态效果设置。

验证命令：

```sh
go test ./...
PVF_TESTFILE=/path/to/90CN.pvf go test ./services -run TestShopReal90CN -count=1
cd frontend
npm test
npm run build
```

真实样例为 `itemshop/101_joann_box.shp` 和 `itemshop/equipmentshop1.shp`。图标需要在应用中配置可用的 NPK 资源目录。
