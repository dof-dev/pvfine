package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"pvfine/internal/mcpserver"
)

type mcpArgs struct {
	WorkspaceID            string   `json:"workspaceId"`
	ExpectedRevision       uint64   `json:"expectedRevision"`
	ExpectedWorkspaceEpoch uint64   `json:"expectedWorkspaceEpoch"`
	RequestID              string   `json:"requestId"`
	Path                   string   `json:"path"`
	Query                  string   `json:"query"`
	Scope                  string   `json:"scope"`
	Mode                   string   `json:"mode"`
	Source                 string   `json:"source"`
	Label                  string   `json:"label"`
	Topic                  string   `json:"topic"`
	TaskID                 string   `json:"taskId"`
	PlanID                 string   `json:"planId"`
	ImportPlanID           string   `json:"importPlanId"`
	ExportPlanID           string   `json:"exportPlanId"`
	Filter                 string   `json:"filter"`
	Cursor                 int      `json:"cursor"`
	Limit                  int      `json:"limit"`
	StartLine              int      `json:"startLine"`
	DirtyPolicy            string   `json:"dirtyPolicy"`
	Overwrite              bool     `json:"overwrite"`
	TargetDigest           string   `json:"targetDigest"`
	ChangeKeys             []string `json:"changeKeys"`
	SelectedKeys           []string `json:"selectedKeys"`
	OverwriteKeys          []string `json:"overwriteKeys"`
	SourcePaths            []string `json:"sourcePaths"`
	TargetDir              string   `json:"targetDir"`
	Scopes                 []string `json:"scopes"`
	Format                 string   `json:"format"`
	Name                   string   `json:"name"`
	Version                string   `json:"version"`
	CommitID               string   `json:"commitId"`
	IncludeDependencies    bool     `json:"includeDependencies"`
	OutputParent           string   `json:"outputParent"`
	OutputName             string   `json:"outputName"`
	SelectedEntryKeys      []string `json:"selectedEntryKeys"`
	AcknowledgedWarningIDs []string `json:"acknowledgedWarningIds"`
}

func (s *MCPService) tools() []mcpserver.Tool {
	properties := map[string]any{}
	for _, key := range strings.Fields("workspaceId requestId path query scope mode source label topic taskId planId importPlanId exportPlanId filter dirtyPolicy targetDigest targetDir format name version commitId outputParent outputName") {
		properties[key] = map[string]any{"type": "string", "maxLength": 4096}
	}
	properties["source"] = map[string]any{"type": "string", "maxLength": 1 << 20}
	for _, key := range strings.Fields("expectedRevision expectedWorkspaceEpoch cursor startLine") {
		properties[key] = map[string]any{"type": "integer", "minimum": 0}
	}
	properties["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
	for _, key := range []string{"overwrite", "includeDependencies"} {
		properties[key] = map[string]any{"type": "boolean"}
	}
	for _, key := range strings.Fields("changeKeys selectedKeys overwriteKeys sourcePaths scopes selectedEntryKeys acknowledgedWarningIds") {
		properties[key] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "maxLength": 4096}, "maxItems": 10000}
	}
	type definition struct {
		name, description, fields, required string
		readonly                            bool
	}
	defs := []definition{
		{"workspace_info", "获取当前 PVF 工作区身份、修订号和未保存状态。首次调用此工具。", "", "", true},
		{"search", "搜索当前归档。mode=path 使用路径通配符，content 使用字符串索引；结果分页。", "workspaceId expectedRevision query scope mode cursor limit", "workspaceId expectedRevision query", true},
		{"read_file", "按归档内部路径读取脚本文本，startLine 从 1 开始；返回分页行，不接受本机路径。", "workspaceId expectedRevision path startLine limit", "workspaceId expectedRevision path", true},
		{"script_help", "查看沙箱 pvf JavaScript API 和示例。topic=overview/files/sections/lists/strings/types；types 返回编辑器同源的完整类型声明。", "topic", "", true},
		{"script_validate", "仅校验 JavaScript 语法，不运行。", "source", "source", true},
		{"script_run", "在隔离事务运行 pvf JavaScript，返回任务 ID；完成后先查看预览再应用。pvf.log 用于查询输出。", "workspaceId expectedRevision source label requestId", "workspaceId expectedRevision source requestId", false},
		{"script_status", "查询脚本任务状态及最终结果（含日志、诊断和 planId）。", "taskId", "taskId", true},
		{"task_status", "查询归档/导入/导出任务状态与最终结果。", "taskId", "taskId", true},
		{"script_preview", "分页查看脚本差异与 changeKey。", "planId filter cursor limit", "planId", true},
		{"script_apply", "应用明确选择的脚本变更到工作区，不自动保存 PVF。字符串表依赖需一起选择。", "workspaceId planId expectedRevision changeKeys requestId", "workspaceId planId expectedRevision changeKeys requestId", false},
		{"script_cancel", "只取消 MCP 发起的指定脚本任务。", "taskId", "taskId", false},
		{"script_discard", "释放指定 MCP 脚本预览。", "planId", "planId", false},
		{"archive_open", "按本机绝对路径打开/切换 PVF。dirtyPolicy=reject/save/discard，默认 reject；始终保护前端草稿。", "path workspaceId expectedRevision expectedWorkspaceEpoch dirtyPolicy requestId", "path expectedWorkspaceEpoch requestId", false},
		{"archive_close", "关闭当前归档，保留 MCP 服务。dirtyPolicy=reject/save/discard。", "workspaceId expectedRevision dirtyPolicy requestId", "workspaceId expectedRevision requestId", false},
		{"archive_save", "保存当前工作区到源文件，检查外部修改。", "workspaceId expectedRevision requestId", "workspaceId expectedRevision requestId", false},
		{"archive_save_as", "将当前内容生成副本，后续保存仍写原文件；保留源文件未保存状态。覆盖已有目标需 overwrite 和 targetDigest。", "workspaceId expectedRevision path overwrite targetDigest requestId", "workspaceId expectedRevision path requestId", false},
		{"import_prepare", "冻结普通文件/目录并预览导入。mode=text/raw；targetDir 是归档内部目录。目录保留所选目录名。", "workspaceId expectedRevision sourcePaths targetDir mode requestId", "workspaceId expectedRevision sourcePaths mode requestId", false},
		{"import_preview", "分页查看冻结导入的路径、覆盖与差异。", "importPlanId cursor limit", "importPlanId", true},
		{"import_apply", "将冻结内容导入工作区，不保存 PVF。selectedKeys 选定条目，覆盖必须同时列入 overwriteKeys。", "workspaceId expectedRevision importPlanId selectedKeys overwriteKeys requestId", "workspaceId expectedRevision importPlanId selectedKeys requestId", false},
		{"import_discard", "释放导入计划。", "importPlanId", "importPlanId", false},
		{"export_formats", "列出可用 MOD 格式及支持操作。", "", "", true},
		{"export_prepare", "准备导出。mode=direct/mod；source=selection/memory/working/commit；MOD 需 format/name，支持依赖补齐；不保存 PVF。", "workspaceId expectedRevision source scopes mode format name version commitId includeDependencies requestId", "workspaceId expectedRevision source mode includeDependencies requestId", false},
		{"export_preview", "分页查看导出树、entryKey 和 warningId。", "exportPlanId cursor limit", "exportPlanId", true},
		{"export_execute", "将已选内容发布到全新目录。direct 需 outputName；MOD 以 name 为目录名，不覆盖。需回传已查看的 warningId。", "exportPlanId outputParent outputName selectedEntryKeys acknowledgedWarningIds requestId", "exportPlanId outputParent selectedEntryKeys requestId", false},
		{"export_discard", "释放导出预览。", "exportPlanId", "exportPlanId", false},
	}
	result := []mcpserver.Tool{}
	for _, def := range defs {
		d := def
		props := map[string]any{}
		for _, key := range strings.Fields(d.fields) {
			props[key] = properties[key]
		}
		schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false, "required": strings.Fields(d.required)}
		result = append(result, mcpserver.Tool{Name: "pvf_" + d.name, Description: d.description, Schema: schema, ReadOnly: d.readonly, Call: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a mcpArgs
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, fmt.Errorf("INVALID_ARGUMENT")
			}
			if a.Limit == 0 {
				a.Limit = 50
			}
			return s.call(ctx, d.name, raw, a)
		}})
	}
	return append(result, s.annotationTools()...)
}
func (s *MCPService) call(ctx context.Context, name string, raw json.RawMessage, a mcpArgs) (any, error) {
	switch name {
	case "workspace_info":
		return s.workspace(), nil
	case "script_help":
		if a.Topic == "types" {
			return map[string]any{"topic": "types", "declarations": s.apiDeclaration}, nil
		}
		return scriptHelp(a.Topic), nil
	case "script_validate":
		return s.script.Compile(a.Source), nil
	case "task_status", "script_status":
		return s.task(a.TaskID)
	case "export_formats":
		return s.export.Formats(), nil
	case "search":
		return s.search(a)
	case "read_file":
		return s.readFile(a)
	case "script_preview":
		if err := s.ownScript(a.PlanID); err != nil {
			return nil, err
		}
		page, err := s.script.PreviewPage(a.PlanID, a.Filter, a.Cursor, a.Limit)
		if err != nil {
			return nil, err
		}
		return boundedScriptPage(page), nil
	case "import_preview":
		return s.importPreview(a)
	case "export_preview":
		return s.exportPreview(a)
	case "script_cancel":
		s.mu.Lock()
		task := s.tasks[a.TaskID]
		own := task != nil && task.Kind == "script_run" && task.Status == "running"
		cancel := s.cancel
		s.mu.Unlock()
		if !own || cancel == nil {
			return nil, fmt.Errorf("TASK_NOT_RUNNING")
		}
		cancel()
		return map[string]any{"cancelRequested": true}, nil
	case "script_discard", "import_discard", "export_discard":
		s.mu.Lock()
		busy := s.active
		s.mu.Unlock()
		if busy {
			return nil, fmt.Errorf("TASK_BUSY")
		}
		switch name {
		case "script_discard":
			if err := s.ownScript(a.PlanID); err != nil {
				return nil, err
			}
			s.script.Discard(a.PlanID)
			s.c.mu.Lock()
			s.c.mcpScriptPlan = ""
			s.c.mu.Unlock()
			emitEvent("mcp:script-cleared", map[string]string{"planId": a.PlanID})
		case "import_discard":
			s.mu.Lock()
			delete(s.imports, a.ImportPlanID)
			s.mu.Unlock()
			s.c.mu.Lock()
			if s.c.mcpImportPlan == a.ImportPlanID {
				s.c.mcpImportPlan = ""
			}
			s.c.mu.Unlock()
		case "export_discard":
			s.mu.Lock()
			id := s.exportID
			if id == a.ExportPlanID {
				s.exportID = ""
			}
			s.mu.Unlock()
			if id != a.ExportPlanID {
				return nil, fmt.Errorf("PLAN_NOT_FOUND")
			}
			s.export.Release(id)
		}
		return map[string]any{"discarded": true}, nil
	}
	return s.submit(name, raw, a.RequestID, func(taskCtx context.Context) (any, error) {
		switch name {
		case "archive_open":
			return s.open(a)
		case "archive_close":
			return s.close(a)
		case "archive_save", "archive_save_as":
			return s.save(a, name == "archive_save_as")
		case "script_run":
			return s.run(taskCtx, a)
		case "script_apply":
			if err := s.check(a); err != nil {
				return nil, err
			}
			if err := s.ownScript(a.PlanID); err != nil {
				return nil, err
			}
			r, err := s.script.Apply(a.PlanID, a.ChangeKeys)
			if err == nil {
				s.c.mu.Lock()
				s.c.mcpScriptPlan = ""
				s.c.mu.Unlock()
				emitEvent("mcp:script-cleared", map[string]string{"planId": a.PlanID})
			}
			return r, err
		case "import_prepare":
			return s.importPrepare(a)
		case "import_apply":
			return s.importApply(a)
		case "export_prepare":
			return s.exportPrepare(a)
		case "export_execute":
			return s.exportExecute(a)
		}
		return nil, fmt.Errorf("UNKNOWN_TOOL")
	})
}
func scriptHelp(topic string) any {
	docs := map[string]string{
		"overview": "沙箱内只有 pvf API；没有 require/process/fetch/文件系统。查询使用 pvf.log(JSON.stringify(result))。修改隔离运行，完成后查看 planId，预览后选择 changeKey 应用。使用文件内部路径，不使用文件索引做长期标识。",
		"files":    "pvf.find(path) 返回文件或 null；pvf.glob(pattern)、pvf.files() 返回文件数组。file.text()/setText(text) 读取/修改；file.parse() 得到结构化文档，修改后 file.write(doc)。pvf.createFile(path,pvf.types.script,text)、copyFile(from,to,overwrite=false)、deleteFile(path)；增删后旧句柄失效，重新查询。",
		"sections": "doc.sections(path) 返回全部同名 section；doc.section(path,occurrence=0) 返回指定项或 null。section.get(index=0)、getValue(index=0)、set(value,index=0)、values()、setValues(values)、append(value)、children()、removeChildren(name)、delete()。doc.set(path,value,{occurrence,valueIndex,create,endTag})、appendSection(name,values,{endTag})。重复 section 应遍历 sections()。值可用精确对象 {type:'integer'|'float'|'string'|'quoted'|'block8',value:...}。",
		"lists":    "pvf.lst(path) 获取列表，支持 .get()、forEach((id,path)=>...)、set(id,path)、mset({id:path})、unset(id)、getId(path)。set 要求目标文件存在；按归档布局处理路径。删除文件不会自动注销列表。",
		"strings":  "doc/section.getOriStrValue 解析字符串引用；pvf.getOriStrValue(reference) 解析 <表号::键名>。set(string) 在支持格式中自动注册字符串引用；setStrValue 强制旧式内联文本。预览中的 .str 依赖必须与引用文件一起选择。文件集修改目前不向 MCP 开放。",
	}
	if topic == "" {
		topic = "overview"
	}
	return map[string]any{"topic": topic, "documentation": docs[topic], "topics": []string{"overview", "files", "sections", "lists", "strings", "types"}, "example": "for (const f of pvf.glob('equipment/**/*.equ')) { const d=f.parse(); const p=d.section('price'); if(p && typeof p.get()==='number' && p.get()<10){p.set(10); f.write(d);} }"}
}
