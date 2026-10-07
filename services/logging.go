package services

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"pvfine/internal/pvf"
)

const logCapacity = 2000

type appLogEntry struct {
	ID        uint64 `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
}

type logBuffer struct {
	mu      sync.Mutex
	nextID  uint64
	entries []appLogEntry
}

var applicationLogs logBuffer

func debugLog(format string, args ...any) {
	recordLog("DEBUG", "performance", fmt.Sprintf(format, args...))
}

func debugPhase(operation, detail string) func() {
	started := time.Now()
	debugLog("%s started %s", operation, detail)
	return func() {
		debugLog("%s finished %s elapsed=%.2fms", operation, detail, elapsedMilliseconds(started))
	}
}

func debugLockAcquired(operation string, started time.Time) {
	debugLog("%s core-lock acquired wait=%.2fms", operation, elapsedMilliseconds(started))
}

var applicationLogStages = struct {
	sync.Mutex
	values map[string]string
}{values: make(map[string]string)}

func (b *logBuffer) append(level, source, message string) appLogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	entry := appLogEntry{b.nextID, time.Now().Format(time.RFC3339Nano), level, source, message}
	if len(b.entries) == logCapacity {
		copy(b.entries, b.entries[1:])
		b.entries[len(b.entries)-1] = entry
	} else {
		b.entries = append(b.entries, entry)
	}
	return entry
}

func (b *logBuffer) snapshot() []appLogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]appLogEntry(nil), b.entries...)
}

func recordLog(level, source, message string) {
	entry := applicationLogs.append(level, source, message)
	if app := application.Get(); app != nil {
		_ = app.Event.Emit("app:log", entry)
	}
}

type logCaptureWriter struct{ output io.Writer }

// CaptureLogs preserves terminal output and forwards existing Go/Wails logs.
func CaptureLogs(output io.Writer) io.Writer {
	return &logCaptureWriter{output: output}
}

func (w *logCaptureWriter) Write(p []byte) (int, error) {
	n, err := w.output.Write(p)
	for _, line := range strings.Split(strings.TrimSpace(string(p)), "\n") {
		if line == "" {
			continue
		}
		recordLog(classifyLogLevel(line), "Go", line)
	}
	return n, err
}

func classifyLogLevel(message string) string {
	lower := strings.ToLower(message)
	// Timing summaries commonly end in error=<nil>; that is not an error.
	lower = strings.ReplaceAll(lower, "error=<nil>", "")
	for _, token := range []string{"level=error", "[err]", "[error]", "err ", "err:", "error=", "failed", "失败"} {
		if strings.Contains(lower, token) {
			return "ERROR"
		}
	}
	for _, token := range []string{"level=warn", "[warn]", "warning", "警告", "skipped:", "unavailable"} {
		if strings.Contains(lower, token) {
			return "WARN"
		}
	}
	if strings.Contains(lower, "level=debug") || strings.Contains(lower, "[debug]") {
		return "DEBUG"
	}
	return "INFO"
}

// LogServiceError observes errors without changing Wails' error serialization.
func LogServiceError(err error) []byte {
	if err != nil {
		log.Printf("[ERR] %v", err)
	}
	return nil
}

// ObserveLogEvents makes startup history available after the frontend subscribes.
func ObserveLogEvents(app *application.App) {
	app.Event.On("app:logs-request", func(*application.CustomEvent) {
		_ = app.Event.Emit("app:logs", applicationLogs.snapshot())
	})
}

// Record before emitting: Wails Go event listeners run asynchronously and may
// observe "ready" before "building" when both events are emitted close together.
func recordLogEvent(name string, data any) {
	applicationLogStages.Lock()
	defer applicationLogStages.Unlock()
	stages := applicationLogStages.values
	if name == "archive:opened" || name == "archive:closed" {
		for key := range stages {
			if strings.HasPrefix(key, "archive:") {
				delete(stages, key)
			}
		}
	}
	level, message, key, signature := describeLogEvent(name, data)
	if message == "" {
		return
	}
	if key != "" {
		if stages[key] == signature {
			return
		}
		stages[key] = signature
	}
	recordLog(level, name, message)
}

func describeLogEvent(name string, data any) (level, message, key, signature string) {
	level = "INFO"
	switch status := data.(type) {
	case pvf.StringTableIndexEvent:
		if status.Kind == "snapshot" {
			return
		}
		kind := "字符串表键值索引"
		if status.Kind == "mapping" {
			kind = "字符串表映射索引"
		}
		message = fmt.Sprintf("%s: 归档=%s; 表=%s; 状态=%s; 条目=%d; 数据=%d B; 本次耗时=%.2f ms; 累计耗时=%.2f ms; 累计表构建=%d; 失败=%d",
			kind, status.ArchivePath, status.Path, status.State, status.Entries, status.Bytes,
			status.DurationMs, status.Stats.BuildDurationMs, status.Stats.TableBuilds, status.Stats.FailedTables)
		if status.Error != "" {
			level = "WARN"
			message += "; 错误=" + status.Error
		}
	case ArchiveInfo:
		action := map[string]string{
			"archive:opened": "归档已打开", "archive:saved": "归档已保存", "archive:reloaded": "归档已重新加载",
		}[name]
		if action == "" {
			return
		}
		size := "未知"
		if stat, err := os.Stat(status.Path); err == nil {
			size = fmt.Sprintf("%d B", stat.Size())
		}
		message = fmt.Sprintf("%s: 路径=%s; 大小=%s; 文件=%d; 分组=%d; 数据体=%d B; 变体=%s; Guard=%t; Paged110=%t; 修改=%d",
			action, status.Path, size, status.FileCount, status.GroupCount, status.BodySize, status.Format, status.UsesGuard, status.Paged110, status.ModifiedCount)
	case IndexStatus:
		key = "archive:index"
		signature = fmt.Sprintf("%s/%s/%t/%s/%s", status.State, status.Stage, status.Refreshing, status.Error, status.RefreshError)
		message = fmt.Sprintf("PVF 索引: 状态=%s; 阶段=%s; 数量=%d/%d; 跳过=%d; 缓存命中=%t; 后台刷新=%t; 打开耗时=%.2f ms; 构建耗时=%.2f ms",
			status.State, status.Stage, status.Done, status.Total, status.Skipped, status.CacheHit, status.Refreshing, status.OpenDurationMs, status.BuildDurationMs)
		if status.Error != "" || status.RefreshError != "" {
			level = "ERROR"
			message += "; 错误=" + status.Error + "; 刷新错误=" + status.RefreshError
		}
	case AdvancedSearchIndexStatus:
		key = "archive:advanced-index"
		signature = status.State + "/" + status.Stage + "/" + status.Error
		message = fmt.Sprintf("字符串反向索引: 状态=%s; 阶段=%s; 文件=%d/%d", status.State, status.Stage, status.Done, status.Total)
		if status.Error != "" {
			level = "ERROR"
			message += "; 错误=" + status.Error
		}
	case ImageIndexStatus:
		key = "image:index"
		signature = fmt.Sprintf("%d/%s/%s/%s", status.Generation, status.State, status.Stage, status.Error)
		message = fmt.Sprintf("NPK 图像索引: 目录=%s; 状态=%s; 阶段=%s; 文件=%d/%d; NPK=%d; IMG=%d; 图像=%d; 跳过=%d; 重复=%d; 耗时=%.2f ms",
			status.Directory, status.State, status.Stage, status.Done, status.Total, status.NPKFiles, status.IMGFiles, status.ImageCount, status.Skipped, status.Duplicates, status.BuildDurationMs)
		if status.Error != "" {
			level = "ERROR"
			message += "; 错误=" + status.Error
		}
	case map[string]any:
		if name == "script:log" {
			level = strings.ToUpper(fmt.Sprint(status["level"]))
			if level == "ERR" {
				level = "ERROR"
			}
			message = fmt.Sprint(status["message"])
		} else if name == "script:state" {
			message = fmt.Sprintf("脚本执行: 状态=%v", status["status"])
			if err, ok := status["error"].(string); ok && err != "" {
				level = "ERROR"
				message += "; 错误=" + err
			}
		} else if name == "unpack:done" {
			message = fmt.Sprint(status["message"])
			level = classifyLogLevel(message)
		} else if name == "archive:batch-applied" {
			message = fmt.Sprintf("批处理已应用: %v", status)
		} else if name == "version:changed" {
			if version, ok := status["status"].(VersionStatus); ok && version.Error != "" {
				level = "ERROR"
				message = "版本状态错误: " + version.Error
			}
		}
	}
	if message == "" {
		message = map[string]string{
			"archive:closed":           "归档已关闭",
			"archive:file-index-ready": "归档文件目录索引已就绪",
			"archive:script-applied":   "脚本修改已应用",
			"version:committed":        "版本已提交",
			"version:checked-out":      "版本已切换",
		}[name]
	}
	return
}
