package script

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"

	"pvfine/internal/pvf"
)

// GojaRuntime executes the restricted PVF script language. A runtime is
// created for each run because Goja runtimes are single-goroutine objects.
type GojaRuntime struct{}

func NewGojaRuntime() *GojaRuntime { return &GojaRuntime{} }

// Compile validates syntax without touching an archive.
func (r *GojaRuntime) Compile(source string) CompileResult {
	if _, err := goja.Compile("pvfine-script.pvf.js", source, false); err != nil {
		return CompileResult{Diagnostics: parseDiagnostics(err)}
	}
	return CompileResult{Valid: true, Diagnostics: []Diagnostic{}}
}

// Run compiles and executes one script in a fresh sandbox.
func (r *GojaRuntime) Run(ctx context.Context, source string, host *BatchAPI) (RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if host == nil {
		return RunResult{}, fmt.Errorf("脚本宿主为空")
	}
	if err := ctx.Err(); err != nil {
		return cancelledRunResult(err), nil
	}
	program, err := goja.Compile("pvfine-script.pvf.js", source, false)
	if err != nil {
		diagnostics := parseDiagnostics(err)
		result := RunResult{Status: RunStatusFailed}
		if len(diagnostics) > 0 {
			result.Error = &diagnostics[0]
		} else {
			result.Error = &Diagnostic{Kind: ErrorKindCompile, Message: err.Error()}
		}
		return result, nil
	}

	vm := goja.New()
	if err := bindRuntime(vm, host); err != nil {
		return RunResult{}, err
	}
	for _, name := range []string{"require", "process", "fetch", "Deno", "WebAssembly"} {
		if err := vm.Set(name, goja.Undefined()); err != nil {
			return RunResult{}, err
		}
	}

	interruptDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-interruptDone:
		}
	}()
	_, runErr := vm.RunProgram(program)
	close(interruptDone)

	result := RunResult{
		Status:        RunStatusCompleted,
		ScannedFiles:  host.ScannedCount(),
		ModifiedFiles: host.ModifiedCount(),
	}
	if runErr != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return cancelledRunResult(contextErr), nil
		}
		diagnostic := runtimeDiagnostic(runErr)
		result.Status = RunStatusFailed
		result.Error = &diagnostic
	}
	return result, nil
}

func cancelledRunResult(err error) RunResult {
	kind := ErrorKindCancelled
	if errors.Is(err, context.DeadlineExceeded) {
		kind = ErrorKindTimeout
	}
	return RunResult{
		Status: RunStatusCancelled,
		Error:  &Diagnostic{Kind: kind, Message: err.Error()},
	}
}

func parseDiagnostics(err error) []Diagnostic {
	result := make([]Diagnostic, 0, 1)
	var list parser.ErrorList
	if errors.As(err, &list) {
		for _, item := range list {
			if item == nil {
				continue
			}
			result = append(result, Diagnostic{
				Kind:    ErrorKindCompile,
				Message: item.Message,
				Line:    item.Position.Line,
				Column:  item.Position.Column,
			})
		}
		return result
	}
	var syntax *goja.CompilerSyntaxError
	if errors.As(err, &syntax) && syntax != nil {
		diagnostic := Diagnostic{Kind: ErrorKindCompile, Message: syntax.Message}
		if syntax.File != nil {
			position := syntax.File.Position(syntax.Offset)
			diagnostic.Line = position.Line
			diagnostic.Column = position.Column
		} else {
			diagnostic.Line, diagnostic.Column = compilerMessagePosition(syntax.Message)
		}
		return []Diagnostic{diagnostic}
	}
	var item *parser.Error
	if errors.As(err, &item) && item != nil {
		return []Diagnostic{{
			Kind:    ErrorKindCompile,
			Message: item.Message,
			Line:    item.Position.Line,
			Column:  item.Position.Column,
		}}
	}
	return []Diagnostic{{Kind: ErrorKindCompile, Message: err.Error()}}
}

func compilerMessagePosition(message string) (int, int) {
	position := strings.Index(message, "Line ")
	if position < 0 {
		return 0, 0
	}
	var line, column int
	if _, err := fmt.Sscanf(message[position:], "Line %d:%d", &line, &column); err != nil {
		return 0, 0
	}
	return line, column
}

func runtimeDiagnostic(err error) Diagnostic {
	diagnostic := Diagnostic{Kind: ErrorKindRuntime, Message: err.Error()}
	if errors.Unwrap(err) != nil {
		diagnostic.Kind = ErrorKindHost
	}
	var exception *goja.Exception
	if errors.As(err, &exception) && exception != nil {
		diagnostic.Message = exception.Error()
		diagnostic.Stack = exception.String()
		frames := exception.Stack()
		if len(frames) > 0 {
			position := frames[0].Position()
			diagnostic.Line = position.Line
			diagnostic.Column = position.Column
		}
	}
	return diagnostic
}

type gojaBindings struct {
	vm   *goja.Runtime
	host *BatchAPI

	files     map[*goja.Object]*FileHandle
	documents map[*goja.Object]*documentBinding
	sections  map[*goja.Object]*sectionBinding
}

type documentBinding struct {
	host     *BatchAPI
	file     *FileHandle
	document *pvf.ScriptDocument
}

type sectionBinding struct {
	host     *BatchAPI
	document *documentBinding
	section  *pvf.ScriptSection
}

func bindRuntime(vm *goja.Runtime, host *BatchAPI) error {
	binding := &gojaBindings{
		vm:        vm,
		host:      host,
		files:     make(map[*goja.Object]*FileHandle),
		documents: make(map[*goja.Object]*documentBinding),
		sections:  make(map[*goja.Object]*sectionBinding),
	}
	if err := binding.bindPVF(); err != nil {
		return err
	}
	return binding.bindConsole()
}

func (b *gojaBindings) bindPVF() error {
	pvfObject := b.vm.NewObject()
	if err := setFunction(b.vm, pvfObject, "files", func(goja.FunctionCall) (goja.Value, error) {
		files, err := b.host.Files()
		if err != nil {
			return nil, err
		}
		items := make([]interface{}, 0, len(files))
		for _, file := range files {
			items = append(items, b.fileObject(file))
		}
		return b.vm.NewArray(items...), nil
	}); err != nil {
		return err
	}
	if err := setFunction(b.vm, pvfObject, "find", func(call goja.FunctionCall) (goja.Value, error) {
		filePath, err := requiredString(call.Argument(0), "路径")
		if err != nil {
			return nil, err
		}
		file, found, err := b.host.Find(filePath)
		if err != nil {
			return nil, err
		}
		if !found {
			return goja.Null(), nil
		}
		return b.fileObject(file), nil
	}); err != nil {
		return err
	}
	if err := setFunction(b.vm, pvfObject, "glob", func(call goja.FunctionCall) (goja.Value, error) {
		pattern, err := requiredString(call.Argument(0), "glob 模式")
		if err != nil {
			return nil, err
		}
		files, err := b.host.Glob(pattern)
		if err != nil {
			return nil, err
		}
		items := make([]interface{}, 0, len(files))
		for _, file := range files {
			items = append(items, b.fileObject(file))
		}
		return b.vm.NewArray(items...), nil
	}); err != nil {
		return err
	}
	if err := setFunction(b.vm, pvfObject, "log", func(call goja.FunctionCall) (goja.Value, error) {
		b.host.Log(LogLevelInfo, formatArguments(call.Arguments))
		return goja.Undefined(), nil
	}); err != nil {
		return err
	}
	if err := setFunction(b.vm, pvfObject, "progress", func(call goja.FunctionCall) (goja.Value, error) {
		done, err := requiredInteger(call.Argument(0), "进度完成数")
		if err != nil {
			return nil, err
		}
		total, err := requiredInteger(call.Argument(1), "进度总数")
		if err != nil {
			return nil, err
		}
		message := ""
		if !goja.IsUndefined(call.Argument(2)) && !goja.IsNull(call.Argument(2)) {
			message, err = requiredString(call.Argument(2), "进度说明")
			if err != nil {
				return nil, err
			}
		}
		if err := b.host.Progress(int(done), int(total), message); err != nil {
			return nil, err
		}
		return goja.Undefined(), nil
	}); err != nil {
		return err
	}
	types := b.vm.NewObject()
	if err := defineReadOnly(types, "script", b.vm.ToValue(pvf.TypeScript)); err != nil {
		return err
	}
	if err := defineReadOnly(types, "unicode", b.vm.ToValue(pvf.TypeUnicode)); err != nil {
		return err
	}
	if err := defineReadOnly(pvfObject, "types", types); err != nil {
		return err
	}
	if err := defineGetter(b.vm, pvfObject, "modifiedCount", func() int { return b.host.ModifiedCount() }); err != nil {
		return err
	}
	if err := defineGetter(b.vm, pvfObject, "scannedCount", func() int { return b.host.ScannedCount() }); err != nil {
		return err
	}
	return b.vm.Set("pvf", pvfObject)
}

func (b *gojaBindings) bindConsole() error {
	console := b.vm.NewObject()
	for level, method := range map[string]string{
		LogLevelInfo:  "log",
		LogLevelWarn:  "warn",
		LogLevelError: "error",
	} {
		selectedLevel := level
		if err := setFunction(b.vm, console, method, func(call goja.FunctionCall) (goja.Value, error) {
			b.host.Log(selectedLevel, formatArguments(call.Arguments))
			return goja.Undefined(), nil
		}); err != nil {
			return err
		}
	}
	return b.vm.Set("console", console)
}

func (b *gojaBindings) fileObject(file *FileHandle) *goja.Object {
	object := b.vm.NewObject()
	b.files[object] = file
	_ = defineReadOnly(object, "index", b.vm.ToValue(file.Index()))
	_ = defineReadOnly(object, "path", b.vm.ToValue(file.Path()))
	_ = defineReadOnly(object, "type", b.vm.ToValue(file.Type()))
	_ = defineReadOnly(object, "size", b.vm.ToValue(file.Size()))
	_ = setFunction(b.vm, object, "text", func(goja.FunctionCall) (goja.Value, error) {
		text, err := file.Text()
		if err != nil {
			return nil, err
		}
		return b.vm.ToValue(text), nil
	})
	_ = setFunction(b.vm, object, "setText", func(call goja.FunctionCall) (goja.Value, error) {
		text, err := requiredString(call.Argument(0), "文本")
		if err != nil {
			return nil, err
		}
		if err := file.SetText(text); err != nil {
			return nil, err
		}
		return goja.Undefined(), nil
	})
	_ = setFunction(b.vm, object, "parse", func(goja.FunctionCall) (goja.Value, error) {
		document, err := file.Parse()
		if err != nil {
			return nil, err
		}
		return b.documentObject(&documentBinding{host: b.host, file: file, document: document}), nil
	})
	_ = setFunction(b.vm, object, "write", func(call goja.FunctionCall) (goja.Value, error) {
		object, ok := call.Argument(0).(*goja.Object)
		if !ok {
			return nil, fmt.Errorf("write 需要 PVFDocument")
		}
		document := b.documents[object]
		if document == nil || document.host != b.host || document.file == nil || document.file.Index() != file.Index() {
			return nil, fmt.Errorf("文档不属于当前文件或事务")
		}
		if err := file.Write(document.document); err != nil {
			return nil, err
		}
		return goja.Undefined(), nil
	})
	return object
}

func (b *gojaBindings) documentObject(document *documentBinding) *goja.Object {
	object := b.vm.NewObject()
	b.documents[object] = document
	_ = setFunction(b.vm, object, "sections", func(call goja.FunctionCall) (goja.Value, error) {
		path, err := sectionPath(call.Argument(0))
		if err != nil {
			return nil, err
		}
		sections := document.document.Sections(path)
		items := make([]interface{}, 0, len(sections))
		for _, section := range sections {
			items = append(items, b.sectionObject(&sectionBinding{host: b.host, document: document, section: section}))
		}
		return b.vm.NewArray(items...), nil
	})
	_ = setFunction(b.vm, object, "section", func(call goja.FunctionCall) (goja.Value, error) {
		path, err := sectionPath(call.Argument(0))
		if err != nil {
			return nil, err
		}
		occurrence, err := optionalInteger(call.Argument(1), 0)
		if err != nil {
			return nil, err
		}
		section, found := document.document.Section(path, int(occurrence))
		if !found {
			return goja.Null(), nil
		}
		return b.sectionObject(&sectionBinding{host: b.host, document: document, section: section}), nil
	})
	_ = setFunction(b.vm, object, "warnings", func(goja.FunctionCall) (goja.Value, error) {
		warnings := document.document.Warnings()
		items := make([]interface{}, 0, len(warnings))
		for _, warning := range warnings {
			item := b.vm.NewObject()
			_ = defineReadOnly(item, "code", b.vm.ToValue(string(warning.Code)))
			_ = defineReadOnly(item, "message", b.vm.ToValue(warning.Message))
			_ = defineReadOnly(item, "tokenIndex", b.vm.ToValue(warning.TokenIndex))
			_ = defineReadOnly(item, "path", b.pathArray(warning.SectionPath))
			items = append(items, item)
		}
		return b.vm.NewArray(items...), nil
	})
	_ = setFunction(b.vm, object, "get", func(call goja.FunctionCall) (goja.Value, error) {
		value, found, err := b.documentValue(document, call, false)
		if err != nil {
			return nil, err
		}
		if !found {
			return goja.Undefined(), nil
		}
		return b.vm.ToValue(value.Value), nil
	})
	_ = setFunction(b.vm, object, "getValue", func(call goja.FunctionCall) (goja.Value, error) {
		value, found, err := b.documentValue(document, call, false)
		if err != nil {
			return nil, err
		}
		if !found {
			return goja.Undefined(), nil
		}
		return b.valueObject(value), nil
	})
	_ = setFunction(b.vm, object, "set", func(call goja.FunctionCall) (goja.Value, error) {
		path, err := sectionPath(call.Argument(0))
		if err != nil {
			return nil, err
		}
		options, err := objectArgument(call.Argument(2))
		if err != nil {
			return nil, err
		}
		occurrence, err := optionInteger(options, "occurrence", 0)
		if err != nil {
			return nil, err
		}
		valueIndex, err := optionInteger(options, "valueIndex", 0)
		if err != nil {
			return nil, err
		}
		create, err := optionBool(options, "create", false)
		if err != nil {
			return nil, err
		}
		endTag, err := optionBool(options, "endTag", false)
		if err != nil {
			return nil, err
		}
		var existing *pvf.ScriptValue
		if section, found := document.document.Section(path, int(occurrence)); found {
			if current, found := section.GetValue(int(valueIndex)); found {
				existing = &current
			}
		}
		value, err := b.scriptValue(call.Argument(1), existing)
		if err != nil {
			return nil, err
		}
		changed, err := document.document.Set(path, int(occurrence), int(valueIndex), value, create, endTag)
		if err != nil {
			return nil, err
		}
		return b.vm.ToValue(changed), nil
	})
	_ = setFunction(b.vm, object, "delete", func(call goja.FunctionCall) (goja.Value, error) {
		path, err := sectionPath(call.Argument(0))
		if err != nil {
			return nil, err
		}
		occurrence, err := optionalInteger(call.Argument(1), 0)
		if err != nil {
			return nil, err
		}
		changed, err := document.document.Delete(path, int(occurrence))
		if err != nil {
			return nil, err
		}
		return b.vm.ToValue(changed), nil
	})
	_ = setFunction(b.vm, object, "appendSection", func(call goja.FunctionCall) (goja.Value, error) {
		name, err := requiredString(call.Argument(0), "section 名称")
		if err != nil {
			return nil, err
		}
		values, err := b.scriptValues(call.Argument(1), nil)
		if err != nil {
			return nil, err
		}
		options, err := objectArgument(call.Argument(2))
		if err != nil {
			return nil, err
		}
		endTag, err := optionBool(options, "endTag", false)
		if err != nil {
			return nil, err
		}
		section, err := document.document.AppendSection(name, values, endTag)
		if err != nil {
			return nil, err
		}
		return b.sectionObject(&sectionBinding{host: b.host, document: document, section: section}), nil
	})
	return object
}

func (b *gojaBindings) sectionObject(binding *sectionBinding) *goja.Object {
	object := b.vm.NewObject()
	b.sections[object] = binding
	_ = defineReadOnly(object, "name", b.vm.ToValue(binding.section.Name()))
	_ = defineReadOnly(object, "hasEndTag", b.vm.ToValue(binding.section.HasEndTag()))
	_ = defineGetter(b.vm, object, "path", func() *goja.Object { return b.pathArray(binding.section.Path()) })
	_ = defineGetter(b.vm, object, "occurrence", func() int { return binding.section.Occurrence() })
	_ = setFunction(b.vm, object, "values", func(goja.FunctionCall) (goja.Value, error) {
		values, err := b.sectionValues(binding)
		if err != nil {
			return nil, err
		}
		return b.vm.NewArray(values...), nil
	})
	_ = setFunction(b.vm, object, "children", func(goja.FunctionCall) (goja.Value, error) {
		if err := b.checkSection(binding); err != nil {
			return nil, err
		}
		children := binding.section.Children()
		items := make([]interface{}, 0, len(children))
		for _, child := range children {
			items = append(items, b.sectionObject(&sectionBinding{host: b.host, document: binding.document, section: child}))
		}
		return b.vm.NewArray(items...), nil
	})
	_ = setFunction(b.vm, object, "get", func(call goja.FunctionCall) (goja.Value, error) {
		value, found, err := b.sectionValue(binding, call.Argument(0), false)
		if err != nil {
			return nil, err
		}
		if !found {
			return goja.Undefined(), nil
		}
		return b.vm.ToValue(value.Value), nil
	})
	_ = setFunction(b.vm, object, "getValue", func(call goja.FunctionCall) (goja.Value, error) {
		value, found, err := b.sectionValue(binding, call.Argument(0), false)
		if err != nil {
			return nil, err
		}
		if !found {
			return goja.Undefined(), nil
		}
		return b.valueObject(value), nil
	})
	_ = setFunction(b.vm, object, "set", func(call goja.FunctionCall) (goja.Value, error) {
		if err := b.checkSection(binding); err != nil {
			return nil, err
		}
		index, err := optionalInteger(call.Argument(1), 0)
		if err != nil {
			return nil, err
		}
		var existing *pvf.ScriptValue
		if current, found := binding.section.GetValue(int(index)); found {
			existing = &current
		}
		value, err := b.scriptValue(call.Argument(0), existing)
		if err != nil {
			return nil, err
		}
		if err := binding.section.Set(value, int(index)); err != nil {
			return nil, err
		}
		return goja.Undefined(), nil
	})
	_ = setFunction(b.vm, object, "setValues", func(call goja.FunctionCall) (goja.Value, error) {
		if err := b.checkSection(binding); err != nil {
			return nil, err
		}
		items, err := arrayArguments(call.Argument(0))
		if err != nil {
			return nil, err
		}
		current := binding.section.Values()
		values := make([]pvf.ScriptValue, 0, len(items))
		for index, item := range items {
			var existing *pvf.ScriptValue
			if index < len(current) {
				existingValue := current[index]
				existing = &existingValue
			}
			value, err := b.scriptValue(item, existing)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		if err := binding.section.SetValues(values); err != nil {
			return nil, err
		}
		return goja.Undefined(), nil
	})
	_ = setFunction(b.vm, object, "append", func(call goja.FunctionCall) (goja.Value, error) {
		if err := b.checkSection(binding); err != nil {
			return nil, err
		}
		value, err := b.scriptValue(call.Argument(0), nil)
		if err != nil {
			return nil, err
		}
		if err := binding.section.Append(value); err != nil {
			return nil, err
		}
		return goja.Undefined(), nil
	})
	_ = setFunction(b.vm, object, "appendSection", func(call goja.FunctionCall) (goja.Value, error) {
		if err := b.checkSection(binding); err != nil {
			return nil, err
		}
		name, err := requiredString(call.Argument(0), "section 名称")
		if err != nil {
			return nil, err
		}
		values, err := b.scriptValues(call.Argument(1), nil)
		if err != nil {
			return nil, err
		}
		options, err := objectArgument(call.Argument(2))
		if err != nil {
			return nil, err
		}
		endTag, err := optionBool(options, "endTag", false)
		if err != nil {
			return nil, err
		}
		section, err := binding.section.AppendSection(name, values, endTag)
		if err != nil {
			return nil, err
		}
		return b.sectionObject(&sectionBinding{host: b.host, document: binding.document, section: section}), nil
	})
	_ = setFunction(b.vm, object, "delete", func(goja.FunctionCall) (goja.Value, error) {
		if err := b.checkSection(binding); err != nil {
			return nil, err
		}
		if err := binding.section.Delete(); err != nil {
			return nil, err
		}
		return goja.Undefined(), nil
	})
	return object
}

func (b *gojaBindings) documentValue(document *documentBinding, call goja.FunctionCall, exact bool) (pvf.ScriptValue, bool, error) {
	path, err := sectionPath(call.Argument(0))
	if err != nil {
		return pvf.ScriptValue{}, false, err
	}
	occurrence, err := optionalInteger(call.Argument(1), 0)
	if err != nil {
		return pvf.ScriptValue{}, false, err
	}
	valueIndex, err := optionalInteger(call.Argument(2), 0)
	if err != nil {
		return pvf.ScriptValue{}, false, err
	}
	value, found := document.document.GetValue(path, int(occurrence), int(valueIndex))
	return value, found, nil
}

func (b *gojaBindings) sectionValues(binding *sectionBinding) ([]interface{}, error) {
	if err := b.checkSection(binding); err != nil {
		return nil, err
	}
	values := binding.section.Values()
	result := make([]interface{}, 0, len(values))
	for _, value := range values {
		result = append(result, b.valueObject(value))
	}
	return result, nil
}

func (b *gojaBindings) sectionValue(binding *sectionBinding, value goja.Value, _ bool) (pvf.ScriptValue, bool, error) {
	if err := b.checkSection(binding); err != nil {
		return pvf.ScriptValue{}, false, err
	}
	index, err := optionalInteger(value, 0)
	if err != nil {
		return pvf.ScriptValue{}, false, err
	}
	result, found := binding.section.GetValue(int(index))
	return result, found, nil
}

func (b *gojaBindings) valueObject(value pvf.ScriptValue) *goja.Object {
	object := b.vm.NewObject()
	_ = defineReadOnly(object, "type", b.vm.ToValue(string(value.Type)))
	_ = defineReadOnly(object, "value", b.vm.ToValue(value.Value))
	if value.Pool != "" && (value.Type == pvf.ScriptTokenString || value.Type == pvf.ScriptTokenQuoted || value.Type == pvf.ScriptTokenBlock5 || value.Type == pvf.ScriptTokenBlock7) {
		_ = defineReadOnly(object, "pool", b.vm.ToValue(string(value.Pool)))
	}
	return object
}

func (b *gojaBindings) scriptValues(value goja.Value, existing []pvf.ScriptValue) ([]pvf.ScriptValue, error) {
	if isMissingValue(value) {
		return []pvf.ScriptValue{}, nil
	}
	items, err := arrayArguments(value)
	if err != nil {
		return nil, err
	}
	result := make([]pvf.ScriptValue, 0, len(items))
	for index, item := range items {
		var current *pvf.ScriptValue
		if index < len(existing) {
			copyValue := existing[index]
			current = &copyValue
		}
		converted, err := b.scriptValue(item, current)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}
	return result, nil
}

func (b *gojaBindings) scriptValue(value goja.Value, existing *pvf.ScriptValue) (pvf.ScriptValue, error) {
	if object, ok := value.(*goja.Object); ok && object != nil && !goja.IsNull(value) {
		kind, err := requiredString(object.Get("type"), "token 类型")
		if err != nil {
			return pvf.ScriptValue{}, fmt.Errorf("精确值缺少 type: %w", err)
		}
		input, err := exportScalar(object.Get("value"))
		if err != nil {
			return pvf.ScriptValue{}, fmt.Errorf("精确值缺少 value: %w", err)
		}
		pool := pvf.ScriptStringPool("")
		if poolValue := object.Get("pool"); !isMissingValue(poolValue) {
			poolName, poolErr := requiredString(poolValue, "字符串池")
			if poolErr != nil {
				return pvf.ScriptValue{}, poolErr
			}
			pool = pvf.ScriptStringPool(poolName)
		}
		return pvf.NewScriptValue(pvf.ScriptTokenType(kind), input, pool)
	}

	input, err := exportScalar(value)
	if err != nil {
		return pvf.ScriptValue{}, err
	}
	if existing != nil {
		return pvf.NewScriptValue(existing.Type, input, existing.Pool)
	}
	if text, ok := input.(string); ok {
		return pvf.NewScriptValue(pvf.ScriptTokenQuoted, text, pvf.ScriptPoolUTF8)
	}
	number, ok := input.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return pvf.ScriptValue{}, fmt.Errorf("值必须是 number 或 string")
	}
	if math.Trunc(number) == number && number >= math.MinInt32 && number <= math.MaxInt32 {
		return pvf.NewScriptValue(pvf.ScriptTokenInteger, int64(number), "")
	}
	return pvf.NewScriptValue(pvf.ScriptTokenFloat, number, "")
}

func (b *gojaBindings) checkSection(binding *sectionBinding) error {
	if binding == nil || binding.host != b.host || binding.section == nil {
		return fmt.Errorf("section 句柄无效")
	}
	return b.host.checkContext()
}

func (b *gojaBindings) pathArray(path []string) *goja.Object {
	items := make([]interface{}, len(path))
	for index, part := range path {
		items[index] = part
	}
	return b.vm.NewArray(items...)
}

func defineReadOnly(object *goja.Object, name string, value goja.Value) error {
	return object.DefineDataProperty(name, value, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE)
}

func setFunction(vm *goja.Runtime, object *goja.Object, name string, fn func(goja.FunctionCall) (goja.Value, error)) error {
	return object.Set(name, func(call goja.FunctionCall) goja.Value {
		value, err := fn(call)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return value
	})
}

func defineGetter(vm *goja.Runtime, object *goja.Object, name string, getter interface{}) error {
	return object.DefineAccessorProperty(name, vm.ToValue(getter), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
}

func requiredString(value goja.Value, label string) (string, error) {
	if isMissingValue(value) {
		return "", fmt.Errorf("%s不能为空", label)
	}
	text, ok := value.Export().(string)
	if !ok {
		return "", fmt.Errorf("%s必须是字符串", label)
	}
	return text, nil
}

func exportScalar(value goja.Value) (any, error) {
	if isMissingValue(value) {
		return nil, fmt.Errorf("值不能是 null 或 undefined")
	}
	exported := value.Export()
	switch scalar := exported.(type) {
	case string:
		return scalar, nil
	case int:
		return float64(scalar), nil
	case int64:
		return float64(scalar), nil
	case float64:
		return scalar, nil
	case float32:
		return float64(scalar), nil
	default:
		return nil, fmt.Errorf("值必须是 number 或 string")
	}
}

func requiredInteger(value goja.Value, label string) (int64, error) {
	if isMissingValue(value) {
		return 0, fmt.Errorf("%s不能为空", label)
	}
	number := value.ToFloat()
	if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
		return 0, fmt.Errorf("%s必须是整数", label)
	}
	return int64(number), nil
}

func optionalInteger(value goja.Value, fallback int64) (int64, error) {
	if isMissingValue(value) {
		return fallback, nil
	}
	return requiredInteger(value, "下标")
}

func objectArgument(value goja.Value) (*goja.Object, error) {
	if isMissingValue(value) {
		return nil, nil
	}
	object, ok := value.(*goja.Object)
	if !ok {
		return nil, fmt.Errorf("options 必须是对象")
	}
	return object, nil
}

func optionInteger(object *goja.Object, name string, fallback int64) (int64, error) {
	if object == nil {
		return fallback, nil
	}
	return optionalInteger(object.Get(name), fallback)
}

func optionBool(object *goja.Object, name string, fallback bool) (bool, error) {
	if object == nil || isMissingValue(object.Get(name)) {
		return fallback, nil
	}
	return object.Get(name).ToBoolean(), nil
}

func arrayArguments(value goja.Value) ([]goja.Value, error) {
	if isMissingValue(value) {
		return nil, fmt.Errorf("values 必须是数组")
	}
	object, ok := value.(*goja.Object)
	if !ok || object.ClassName() != "Array" {
		return nil, fmt.Errorf("values 必须是数组")
	}
	length, err := requiredInteger(object.Get("length"), "数组长度")
	if err != nil || length < 0 || length > 1_000_000 {
		return nil, fmt.Errorf("数组长度无效")
	}
	result := make([]goja.Value, 0, length)
	for index := int64(0); index < length; index++ {
		result = append(result, object.Get(fmt.Sprintf("%d", index)))
	}
	return result, nil
}

func sectionPath(value goja.Value) ([]string, error) {
	if isMissingValue(value) {
		return nil, fmt.Errorf("section 路径不能为空")
	}
	if text, ok := value.Export().(string); ok {
		parts := strings.Split(strings.ReplaceAll(text, "\\", "/"), "/")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(strings.Trim(part, "[]"))
			part = strings.TrimPrefix(part, "/")
			if part != "" {
				result = append(result, part)
			}
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("section 路径不能为空")
		}
		return result, nil
	}
	items, err := arrayArguments(value)
	if err != nil {
		return nil, fmt.Errorf("section 路径必须是字符串或字符串数组")
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		part, err := requiredString(item, "section 路径项")
		if err != nil {
			return nil, err
		}
		part = strings.TrimSpace(strings.Trim(part, "[]"))
		part = strings.TrimPrefix(part, "/")
		if part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("section 路径不能为空")
	}
	return result, nil
}

func isMissingValue(value goja.Value) bool {
	return value == nil || goja.IsUndefined(value) || goja.IsNull(value)
}

func formatArguments(arguments []goja.Value) string {
	parts := make([]string, len(arguments))
	for index, value := range arguments {
		parts[index] = value.String()
	}
	return strings.Join(parts, " ")
}
