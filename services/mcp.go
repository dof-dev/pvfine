package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"pvfine/internal/mcpserver"
)

const mcpTTL = 10 * time.Minute

type MCPStatus struct {
	Enabled  bool   `json:"enabled"`
	Port     int    `json:"port"`
	Endpoint string `json:"endpoint"`
	Error    string `json:"error,omitempty"`
	Busy     bool   `json:"busy"`
}
type mcpConfig struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}
type MCPTask struct {
	Progress any `json:"progress,omitempty"`

	ID         string     `json:"taskId"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Result     any        `json:"result,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}
type mcpRequestRecord struct {
	digest [32]byte
	taskID string
}
type mcpWindow struct {
	seen  time.Time
	dirty bool
	busy  bool
}
type mcpAck struct {
	ID    string
	Dirty bool
	Busy  bool
}

// MCPService uses exactly the same archive, script and export services as Wails.
// Local connections share tasks and plans.
type MCPService struct {
	apiDeclaration string

	configMu sync.Mutex

	c          *core
	archive    *ArchiveService
	editor     *EditorService
	script     *ScriptService
	export     *ExportService
	configPath string
	mu         sync.Mutex
	config     mcpConfig
	server     *http.Server
	lastError  string
	tasks      map[string]*MCPTask
	requests   map[string]mcpRequestRecord
	windows    map[string]mcpWindow
	ack        chan mcpAck
	operation  string
	active     bool
	stopping   bool
	wg         sync.WaitGroup
	cancel     context.CancelFunc
	imports    map[string]*mcpImportPlan
	exportID   string
}

func NewMCPService(c *core, a *ArchiveService, e *EditorService, s *ScriptService, x *ExportService, declarations ...string) *MCPService {
	root, err := os.UserConfigDir()
	service := newMCPService(c, a, e, s, x, filepath.Join(root, "pvfine", "mcp.json"))
	if len(declarations) > 0 {
		service.apiDeclaration = declarations[0]
	}
	if err != nil {
		service.lastError = "无法确定 MCP 配置目录"
	}
	return service
}
func newMCPService(c *core, a *ArchiveService, e *EditorService, s *ScriptService, x *ExportService, path string) *MCPService {
	return &MCPService{c: c, archive: a, editor: e, script: s, export: x, configPath: path, config: mcpConfig{Port: 19245}, tasks: map[string]*MCPTask{}, requests: map[string]mcpRequestRecord{}, windows: map[string]mcpWindow{}, imports: map[string]*mcpImportPlan{}}
}
func mcpID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("随机源不可用")
	}
	return hex.EncodeToString(b[:])
}
func (s *MCPService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if app := application.Get(); app != nil {
		for _, name := range []string{"script:progress", "archive:save-progress"} {
			app.Event.On(name, func(event *application.CustomEvent) {
				s.mu.Lock()
				defer s.mu.Unlock()
				for _, task := range s.tasks {
					if task.Status == "running" {
						task.Progress = event.Data
					}
				}
			})
		}
	}

	data, err := os.ReadFile(s.configPath)
	if err == nil {
		if json.Unmarshal(data, &s.config) != nil {
			s.lastError = "MCP 配置无效"
			return nil
		}
	} else if !os.IsNotExist(err) {
		s.lastError = "无法读取 MCP 配置"
		return nil
	}
	if s.config.Enabled {
		_, err = s.Configure(true, s.config.Port)
		if err != nil {
			s.mu.Lock()
			s.lastError = err.Error()
			s.mu.Unlock()
		}
	}
	return nil
}
func (s *MCPService) ServiceShutdown() error { return s.stop() }
func (s *MCPService) stop() error {
	s.mu.Lock()
	s.stopping = true
	server := s.server
	s.server = nil
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	s.wg.Wait()
	s.releasePlans()
	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()
	return nil
}
func (s *MCPService) Status() MCPStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := MCPStatus{Enabled: s.server != nil, Port: s.config.Port, Error: s.lastError, Busy: s.active}
	if result.Enabled {
		result.Endpoint = fmt.Sprintf("http://127.0.0.1:%d/mcp", result.Port)
	}
	return result
}
func (s *MCPService) Configure(enabled bool, port int) (MCPStatus, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if port < 1024 || port > 65535 {
		return s.Status(), fmt.Errorf("端口需在 1024-65535 之间")
	}
	s.mu.Lock()
	busy := s.active
	s.mu.Unlock()
	if busy {
		return s.Status(), fmt.Errorf("TASK_BUSY: 请等待当前任务完成")
	}
	_ = s.stop()
	s.mu.Lock()
	s.tasks = map[string]*MCPTask{}
	s.requests = map[string]mcpRequestRecord{}
	s.mu.Unlock()
	cfg := mcpConfig{Enabled: enabled, Port: port}
	if enabled {
		listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return s.Status(), fmt.Errorf("MCP 端口不可用")
		}
		handler := mcpserver.Handler(s.tools())
		s.mu.Lock()
		s.config = cfg
		s.lastError = ""
		s.server = mcpserver.Serve(listener, handler)
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		s.config = cfg
		s.lastError = ""
		s.mu.Unlock()
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0700); err != nil {
		return s.Status(), fmt.Errorf("保存 MCP 配置失败")
	}
	if err := os.WriteFile(s.configPath, data, 0600); err != nil {
		return s.Status(), fmt.Errorf("保存 MCP 配置失败")
	}
	return s.Status(), nil
}

// ConnectionConfig is only called by the explicit UI copy action. It must never
// be logged or exposed as an MCP tool. The caller copies it to the clipboard.
func (s *MCPService) ConnectionConfig() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return "", fmt.Errorf("MCP 服务未启用")
	}
	value := map[string]any{"mcpServers": map[string]any{"pvfine": map[string]any{"url": fmt.Sprintf("http://127.0.0.1:%d/mcp", s.config.Port)}}}
	data, _ := json.MarshalIndent(value, "", "  ")
	return string(data), nil
}
func (s *MCPService) RegisterWindow(id string) {
	if len(id) > 80 {
		return
	}
	s.mu.Lock()
	old := s.windows[id]
	old.seen = time.Now()
	s.windows[id] = old
	s.mu.Unlock()
}
func (s *MCPService) UnregisterWindow(id string) { s.mu.Lock(); delete(s.windows, id); s.mu.Unlock() }
func (s *MCPService) Acknowledge(windowID, operationID string, dirty, busy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.operation != operationID || s.ack == nil {
		return
	}
	if _, ok := s.windows[windowID]; !ok {
		return
	}
	select {
	case s.ack <- mcpAck{windowID, dirty, busy}:
	default:
	}
}
func (s *MCPService) prepareUI(ctx context.Context, id string) error {
	s.mu.Lock()
	expected := map[string]bool{}
	for key, w := range s.windows {
		if app := application.Get(); app != nil {
			if _, exists := app.Window.GetByName(key); !exists {
				delete(s.windows, key)
				continue
			}
		}
		if time.Since(w.seen) < 12*time.Second {
			expected[key] = true
		} else {
			s.mu.Unlock()
			return fmt.Errorf("UI_NOT_READY: 窗口状态已过期")
		}
	}
	// Headless service tests have no Wails runtime. A live app requires its main window.
	if application.Get() != nil && !expected["main"] {
		s.mu.Unlock()
		return fmt.Errorf("UI_NOT_READY: 等待界面就绪")
	}
	s.operation = id
	s.ack = make(chan mcpAck, len(expected)+1)
	ch := s.ack
	s.mu.Unlock()
	s.mu.Lock()
	kind := ""
	if task := s.tasks[id]; task != nil {
		kind = task.Kind
	}
	s.mu.Unlock()
	emitEvent("mcp:prepare", map[string]any{"operationId": id, "kind": kind})
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for len(expected) > 0 {
		select {
		case a := <-ch:
			if !expected[a.ID] {
				continue
			}
			delete(expected, a.ID)
			if a.Dirty {
				return fmt.Errorf("UI_DRAFT_PENDING: 请先提交或处理编辑器草稿")
			}
			if a.Busy {
				return fmt.Errorf("TASK_BUSY: 界面有未完成的操作")
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("UI_NOT_READY: 窗口未响应")
		}
	}
	return nil
}
func (s *MCPService) finishUI(id string) {
	s.mu.Lock()
	s.operation = ""
	s.ack = nil
	s.mu.Unlock()
	emitEvent("mcp:released", map[string]any{"operationId": id})
}
func (s *MCPService) submit(kind string, raw json.RawMessage, requestID string, work func(context.Context) (any, error)) (any, error) {
	if requestID == "" || len(requestID) > 128 {
		return nil, fmt.Errorf("INVALID_ARGUMENT: requestId 必填，最多 128 字符")
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return nil, fmt.Errorf("INVALID_ARGUMENT")
	}
	normalized, _ := json.Marshal(canonical)
	digest := sha256.Sum256(append([]byte(kind+":"), normalized...))
	s.mu.Lock()
	if old, ok := s.requests[requestID]; ok {
		s.mu.Unlock()
		if old.digest != digest {
			return nil, fmt.Errorf("REQUEST_CONFLICT: requestId 已用于其他参数")
		}
		return map[string]any{"taskId": old.taskID}, nil
	}
	if s.active || s.stopping {
		s.mu.Unlock()
		return nil, fmt.Errorf("TASK_BUSY: 已有任务运行中")
	}
	// Bound retained history; IDs are never automatically replayed after eviction.
	if len(s.tasks) >= 256 {
		s.mu.Unlock()
		return nil, fmt.Errorf("TASK_LIMIT: 请停用后重新启用服务以清理任务历史")
	}
	id := mcpID()
	task := &MCPTask{ID: id, Kind: kind, Status: "running", StartedAt: time.Now()}
	s.tasks[id] = task
	s.requests[requestID] = mcpRequestRecord{digest, id}
	s.active = true
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer cancel()
		var value any
		var err error
		didStart := false
		value, err = func() (v any, e error) {
			defer func() {
				if recover() != nil {
					v = nil
					e = fmt.Errorf("INTERNAL: 任务异常终止")
				}
			}()
			if e = s.prepareUI(ctx, id); e != nil {
				return nil, e
			}
			didStart = true
			return work(ctx)
		}()
		if didStart && (err == nil || (kind != "archive_open" && kind != "archive_close")) {
			switch kind {
			case "script_apply", "import_apply", "archive_save", "archive_open", "archive_close":
				emitEvent("mcp:workspace-changed", map[string]string{"kind": kind})
			}
		}
		s.finishUI(id)
		now := time.Now()
		s.mu.Lock()
		task.FinishedAt = &now
		task.Result = value
		task.Status = "completed"
		if err != nil {
			task.Status = "failed"
			task.Error = err.Error()
		}
		s.active = false
		s.cancel = nil
		s.mu.Unlock()
		notice := map[string]any{"taskId": id, "kind": kind, "status": task.Status}
		if err != nil {
			notice["error"] = err.Error()
		}
		if result, ok := value.(map[string]any); ok {
			for _, key := range []string{"outputPath", "writtenPath"} {
				if path, ok := result[key].(string); ok {
					notice["outputPath"] = path
				}
			}
		}
		emitEvent("mcp:task-finished", notice)
	}()
	return map[string]any{"taskId": id}, nil
}
func (s *MCPService) task(id string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("TASK_NOT_FOUND")
	}
	copy := *t
	return copy, nil
}
func (s *MCPService) releasePlans() {
	s.mu.Lock()
	s.imports = map[string]*mcpImportPlan{}
	exportID := s.exportID
	s.exportID = ""
	s.mu.Unlock()
	if exportID != "" {
		s.export.Release(exportID)
	}
	s.c.mu.Lock()
	id := s.c.mcpScriptPlan
	s.c.mcpScriptPlan = ""
	s.c.mcpImportPlan = ""
	s.c.mu.Unlock()
	if id != "" {
		s.script.Discard(id)
	}
}

// WindowState lets a reloaded webview release a stale preparation barrier.
func (s *MCPService) WindowState() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kind := ""
	if task := s.tasks[s.operation]; task != nil {
		kind = task.Kind
	}
	return map[string]string{"operationId": s.operation, "kind": kind}
}

// CurrentScriptResult restores a retained AI preview after a window reload.
func (s *MCPService) CurrentScriptResult() *ScriptRunResult {
	s.c.mu.RLock()
	id := s.c.mcpScriptPlan
	s.c.mu.RUnlock()
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, task := range s.tasks {
		if r, ok := task.Result.(ScriptRunResult); ok && r.PlanID == id {
			return &r
		}
	}
	return nil
}

// ReportWindow refreshes draft status without persisting editor content.
func (s *MCPService) ReportWindow(id string, dirty, busy bool) {
	if len(id) > 80 {
		return
	}
	s.mu.Lock()
	s.windows[id] = mcpWindow{seen: time.Now(), dirty: dirty, busy: busy}
	s.mu.Unlock()
}

// CancelActiveScript is the local UI escape hatch while an AI script is running.
func (s *MCPService) CancelActiveScript() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, task := range s.tasks {
		if task.Status == "running" && task.Kind == "script_run" && s.cancel != nil {
			s.cancel()
			return true
		}
	}
	return false
}
