# MCP 操作流程

此 skill 面向正在运行的 pvfine MCP 服务。安装 skill 不会自动配置连接；先在 pvfine「偏好设置 → MCP 服务」启用服务，将连接配置加入 AI 客户端。以下是服务端工具名，客户端可能增加前缀。

1. 调用 `pvf_workspace_info`，确认归档、workspaceId、revision 和 versionLoading。未打开归档时先根据用户指定的本机绝对路径使用 `pvf_archive_open`，传入 workspaceEpoch 作为 expectedWorkspaceEpoch；不擅自丢弃未保存内容。
2. 查询用 `pvf_search`（mode=path/content）、`pvf_read_file`。查询读取工具传入 workspaceId 与 expectedRevision=revision。read_file 每页最多 100 行，根据 nextLine 继续；搜索按返回分页继续，不能把第一页当作完整结果。
3. 不清楚字段时用 `pvf_annotation_search`（filePath、section、workspaceId），再以返回的 documentId/rulesRevision 调用 `pvf_annotation_get`。用 `pvf_annotation_explain` 查看当前文件的枚举、字符串引用和关联目标。说明缺失时先读取实际结构，不猜测字段含义。
4. 编写脚本前读取 `pvf_script_help(topic="types")` 获取当前完整声明，必要时读取 sections/lists/strings 帮助。沙箱只有 `pvf`，没有 Node、网络或本机文件系统；查询通过 `pvf.log(JSON.stringify(result))` 输出，限定文件范围，避免输出整个归档。
5. 脚本先 `pvf_script_validate(source)`；通过后 `pvf_script_run(workspaceId, expectedRevision, source, label, requestId)`。轮询 `pvf_script_status(taskId)` 至完成，检查日志与诊断，取得 planId。查询脚本无修改时可以没有 planId。
6. 有变更时用 `pvf_script_preview(planId, cursor, limit)` 看完全部差异，核对文件、字段、旧值、新值与范围。只应用符合用户指令的 changeKey；引用新增 .str 文本时一并选择对应字符串表依赖。调用 `pvf_script_apply(workspaceId, expectedRevision, planId, changeKeys, requestId)` 后用 `pvf_task_status` 等待结束。
7. 应用仅修改内存工作区。重新获取 workspace_info 并读取验证；用户要求保存时才调用 `pvf_archive_save`。`pvf_archive_save_as` 生成副本，后续保存仍写源归档。最终明确说明修改路径、数值、应用状态和磁盘保存状态。

每个新修改请求使用唯一 requestId；同一次请求的网络重试复用原 ID。遇到 STALE_WORKSPACE 重新查询和生成预览，不用新修订号强行应用旧计划。TASK_BUSY、前端草稿或冲突时先处理阻塞；失败不盲目重试。废弃预览用 pvf_script_discard。

## 脚本读写

文件路径是归档内部路径。`pvf.find(path)` 返回句柄或 null；`file.parse()` 得到文档，`doc.sections(path)` 返回全部同名 section，路径与嵌套参数以当前 types 声明为准。`section.get(i)`/`set(value,i)` 访问零基索引；`values()`/`setValues()` 操作直接值；修改文档后须 `file.write(doc)`。每次先验证 section 数量、长度、类型与旧值，再改目标字段。不要用 setValues 重建含子 section 的复合块。必要时用 file.text()/setText() 做精确的局部文本替换，保留未涉及内容、类型标记和闭合标签。

读取 `<表号::键名>` 用 `getOriStrValue`；修改时按当前 strings API 保留引用语义。ID 不等于文件索引；使用标注关联或 `pvf.lst(path)` 解析路径。新建文件要同步正确 lst 注册，删除文件不会自动注销。MCP 不支持文件集修改。
