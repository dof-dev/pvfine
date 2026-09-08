package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	annotationrules "pvfine/internal/annotations"
	"pvfine/internal/pvf"
)

const maxRequestBytes = 4 << 20

type editorServer struct {
	mu          sync.Mutex
	rulesPath   string
	runtimePath string
	htmlPath    string
	token       string
}

type previewRequest struct {
	Path     string                    `json:"path"`
	Text     string                    `json:"text"`
	Document *annotationrules.Document `json:"document,omitempty"`
}

func main() {
	root, err := findRepoRoot()
	if err != nil {
		log.Fatal(err)
	}
	server, err := newEditorServer(root)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{
		Handler:           server.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	url := "http://" + listener.Addr().String() + "/"
	fmt.Printf("标注规则编辑器已启动: %s\n", url)
	if err := openBrowser(url); err != nil {
		fmt.Printf("无法自动打开浏览器，请手动访问上面的地址: %v\n", err)
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("规则编辑器服务异常: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

func newEditorServer(root string) (*editorServer, error) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	runtimePath, err := annotationrules.RuntimePath()
	if err != nil {
		return nil, err
	}
	return &editorServer{
		rulesPath:   filepath.Join(root, "internal", "annotations", "default.json"),
		runtimePath: runtimePath,
		htmlPath:    filepath.Join(root, "tools", "annotation-editor.html"),
		token:       hex.EncodeToString(tokenBytes),
	}, nil
}

func (s *editorServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serveHTML)
	mux.Handle("/api/rules", s.authorized(http.HandlerFunc(s.handleRules)))
	mux.Handle("/api/preview", s.authorized(http.HandlerFunc(s.handlePreview)))
	return mux
}

func (s *editorServer) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Annotation-Editor-Token") != s.token {
			writeAPIError(w, http.StatusForbidden, "无效的编辑器会话")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *editorServer) serveHTML(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(s.htmlPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	page := strings.ReplaceAll(string(data), "__EDITOR_TOKEN__", s.token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, page)
}

func (s *editorServer) handleRules(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(s.rulesPath)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := annotationrules.Parse(data); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	case http.MethodPut:
		data, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(data) > maxRequestBytes {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "规则文件超过 4MB")
			return
		}
		document, err := annotationrules.Parse(data)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		formatted, err := annotationrules.Marshal(document)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := writeRulesWithBackup(s.rulesPath, formatted); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if s.runtimePath != "" {
			if err := atomicWrite(s.runtimePath, formatted, 0o600); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "同步运行时规则失败: "+err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bytes": len(formatted)})
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeAPIError(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func (s *editorServer) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request previewRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "预览参数无效: "+err.Error())
		return
	}

	var document annotationrules.Document
	if request.Document != nil {
		document = *request.Document
		if err := annotationrules.Validate(document); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		s.mu.Lock()
		data, err := os.ReadFile(s.rulesPath)
		s.mu.Unlock()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		document, err = annotationrules.Parse(data)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	engine, err := annotationrules.Compile(document)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results := engine.Annotate(request.Path, pvf.ParseScriptView(request.Text), nil)
	pathResults := engine.AnnotatePath(request.Path, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"annotations":     results,
		"pathAnnotations": pathResults,
	})
}

func writeRulesWithBackup(path string, data []byte) error {
	previous, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := atomicWrite(path+".bak", previous, 0o644); err != nil {
			return fmt.Errorf("写入规则备份失败: %w", err)
		}
	}
	if err := atomicWrite(path, data, 0o644); err != nil {
		return fmt.Errorf("写入规则失败: %w", err)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".annotation-rules-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到包含 go.mod 的项目根目录")
		}
		dir = parent
	}
}

func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
}
