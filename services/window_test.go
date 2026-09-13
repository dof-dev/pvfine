package services

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type fakeScriptWindow struct {
	focusCount int
	closeCount int
	// hook 保存注册进来的 WindowClosing 钩子，便于在测试里模拟关窗。
	hook func(event *application.WindowEvent)
}

func (w *fakeScriptWindow) Focus() { w.focusCount++ }

func (w *fakeScriptWindow) Close() { w.closeCount++ }

func (w *fakeScriptWindow) RegisterHook(
	eventType events.WindowEventType,
	callback func(event *application.WindowEvent),
) func() {
	if eventType == events.Common.WindowClosing {
		w.hook = callback
	}
	return func() { w.hook = nil }
}

type fakeScriptWindowHost struct {
	windows     map[string]*fakeScriptWindow
	createCount int
	lastOptions application.WebviewWindowOptions
}

func newFakeScriptWindowHost() *fakeScriptWindowHost {
	return &fakeScriptWindowHost{windows: map[string]*fakeScriptWindow{}}
}

func (h *fakeScriptWindowHost) findWindow(name string) (scriptWindowHandle, bool) {
	window, ok := h.windows[name]
	if !ok {
		return nil, false
	}
	return window, true
}

func (h *fakeScriptWindowHost) createWindow(
	options application.WebviewWindowOptions,
) (scriptWindowHandle, error) {
	h.createCount++
	h.lastOptions = options
	window := &fakeScriptWindow{}
	h.windows[options.Name] = window
	return window, nil
}

func TestOpenScriptWindowCreatesWindowOnce(t *testing.T) {
	host := newFakeScriptWindowHost()
	service := newScriptWindowService(host)

	session := ScriptSession{Name: "a.pvf.js", Source: "src", SavedSource: "saved"}
	if err := service.OpenScriptWindow(session); err != nil {
		t.Fatalf("OpenScriptWindow() error = %v", err)
	}
	if host.createCount != 1 {
		t.Fatalf("createCount = %d, want 1", host.createCount)
	}

	// 第二次调用必须聚焦已有窗口，而不是再开一个。
	if err := service.OpenScriptWindow(session); err != nil {
		t.Fatalf("second OpenScriptWindow() error = %v", err)
	}
	if host.createCount != 1 {
		t.Errorf("createCount = %d after second call, want 1", host.createCount)
	}
	window := host.windows[ScriptWindowName]
	if window.focusCount != 1 {
		t.Errorf("focusCount = %d, want 1", window.focusCount)
	}
}

func TestOpenScriptWindowUsesQueryViewAndName(t *testing.T) {
	host := newFakeScriptWindowHost()
	service := newScriptWindowService(host)

	if err := service.OpenScriptWindow(ScriptSession{}); err != nil {
		t.Fatalf("OpenScriptWindow() error = %v", err)
	}
	// 名称必须显式设置，否则 Wails 会用 window-N，按名字查不到窗口。
	if host.lastOptions.Name != ScriptWindowName {
		t.Errorf("Name = %q, want %q", host.lastOptions.Name, ScriptWindowName)
	}
	// 资产服务器没有 SPA 回退，视图只能通过 query 选择。
	if host.lastOptions.URL != "/?view=script" {
		t.Errorf("URL = %q, want %q", host.lastOptions.URL, "/?view=script")
	}
}

func TestScriptSessionRoundTrip(t *testing.T) {
	host := newFakeScriptWindowHost()
	service := newScriptWindowService(host)

	if got := service.LoadScriptSession(); got != nil {
		t.Fatalf("LoadScriptSession() = %v before staging, want nil", got)
	}

	want := ScriptSession{Name: "b.pvf.js", Source: "new", SavedSource: "old"}
	if err := service.OpenScriptWindow(want); err != nil {
		t.Fatalf("OpenScriptWindow() error = %v", err)
	}

	got := service.LoadScriptSession()
	if got == nil {
		t.Fatal("LoadScriptSession() = nil, want session")
	}
	if *got != want {
		t.Errorf("LoadScriptSession() = %+v, want %+v", *got, want)
	}

	// 返回的是副本：调用方改动不应影响服务内部状态。
	got.Source = "mutated"
	if again := service.LoadScriptSession(); again == nil || again.Source != "new" {
		t.Errorf("stored session was mutated by caller: %+v", again)
	}
}

func TestScriptWindowCloseHookBlocksUntilAllowed(t *testing.T) {
	host := newFakeScriptWindowHost()
	service := newScriptWindowService(host)

	if err := service.OpenScriptWindow(ScriptSession{}); err != nil {
		t.Fatalf("OpenScriptWindow() error = %v", err)
	}
	window := host.windows[ScriptWindowName]
	if window.hook == nil {
		t.Fatal("WindowClosing hook was not registered")
	}

	// 用户主动关窗：钩子应拦下原生关闭，交给前端确认。
	blocked := application.NewWindowEvent()
	window.hook(blocked)
	if !blocked.IsCancelled() {
		t.Error("hook did not cancel the close, want cancelled for confirmation")
	}

	// 前端确认后调用关闭：钩子应放行。
	if err := service.CloseScriptWindow(ScriptSession{Name: "c.pvf.js"}); err != nil {
		t.Fatalf("CloseScriptWindow() error = %v", err)
	}
	if window.closeCount != 1 {
		t.Errorf("closeCount = %d, want 1", window.closeCount)
	}
	allowed := application.NewWindowEvent()
	window.hook(allowed)
	if allowed.IsCancelled() {
		t.Error("hook cancelled an approved close, want allowed")
	}

	// 放行标志是一次性的：下一个关闭请求必须重新走确认。
	again := application.NewWindowEvent()
	window.hook(again)
	if !again.IsCancelled() {
		t.Error("close approval leaked into the next close request")
	}
}

func TestCloseScriptWindowWithoutWindowIsSafe(t *testing.T) {
	service := newScriptWindowService(newFakeScriptWindowHost())
	// 窗口未打开时关闭不应 panic，且仍应记住交回的会话。
	if err := service.CloseScriptWindow(ScriptSession{Name: "d.pvf.js", Source: "s"}); err != nil {
		t.Fatalf("CloseScriptWindow() error = %v", err)
	}
	if got := service.LoadScriptSession(); got == nil || got.Name != "d.pvf.js" {
		t.Errorf("LoadScriptSession() = %+v, want staged session", got)
	}
	service.CloseScriptWindowIfOpen()
}

func TestFocusScriptWindowReportsExistence(t *testing.T) {
	host := newFakeScriptWindowHost()
	service := newScriptWindowService(host)

	if service.FocusScriptWindow() {
		t.Error("FocusScriptWindow() = true with no window, want false")
	}
	if service.IsScriptWindowOpen() {
		t.Error("IsScriptWindowOpen() = true with no window, want false")
	}

	if err := service.OpenScriptWindow(ScriptSession{}); err != nil {
		t.Fatalf("OpenScriptWindow() error = %v", err)
	}
	if !service.IsScriptWindowOpen() {
		t.Error("IsScriptWindowOpen() = false after open, want true")
	}
	if !service.FocusScriptWindow() {
		t.Error("FocusScriptWindow() = false after open, want true")
	}
	if host.windows[ScriptWindowName].focusCount != 1 {
		t.Errorf("focusCount = %d, want 1", host.windows[ScriptWindowName].focusCount)
	}
}

// 应用退出流程必须能放行脚本窗口，否则退出会卡在它的关闭确认上。
func TestAllowScriptWindowCloseReleasesHook(t *testing.T) {
	host := newFakeScriptWindowHost()
	service := newScriptWindowService(host)

	if err := service.OpenScriptWindow(ScriptSession{}); err != nil {
		t.Fatalf("OpenScriptWindow() error = %v", err)
	}
	window := host.windows[ScriptWindowName]

	service.AllowScriptWindowClose()
	service.CloseScriptWindowIfOpen()
	if window.closeCount != 1 {
		t.Fatalf("closeCount = %d, want 1", window.closeCount)
	}
	event := application.NewWindowEvent()
	window.hook(event)
	if event.IsCancelled() {
		t.Error("hook cancelled the quit-path close, want allowed")
	}
}

func TestScriptWindowServiceWithoutHostIsSafe(t *testing.T) {
	service := newScriptWindowService(nil)
	if err := service.OpenScriptWindow(ScriptSession{}); err == nil {
		t.Error("OpenScriptWindow() with nil host = nil error, want error")
	}
	// 其余方法在无窗口环境下不应 panic。
	service.AllowScriptWindowClose()
	service.CloseScriptWindowIfOpen()
	if service.FocusScriptWindow() {
		t.Error("FocusScriptWindow() = true with nil host, want false")
	}
	if got := service.LoadScriptSession(); got != nil {
		t.Errorf("LoadScriptSession() = %+v, want nil", got)
	}
}
