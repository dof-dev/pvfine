package mod

import (
	"fmt"
	"io/fs"
	"sort"
)

type Descriptor struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	CanRead    bool        `json:"canRead"`
	CanWrite   bool        `json:"canWrite"`
	Operations []Operation `json:"operations"`
}

type Reader interface {
	Detect(fs.FS) (bool, error)
	Read(fs.FS) (Package, error)
}

type Writer interface {
	Validate(Package) error
	Files(Package) []OutputFile
	Write(root string, pkg Package) error
}

// OutputFile maps a physical output back to its removable package entry.
// An empty EntryPath marks generated metadata that must remain in the package.
type OutputFile struct {
	Path      string
	EntryPath string
}

type Format struct {
	Descriptor Descriptor
	Reader     Reader
	Writer     Writer
}

type Registry struct{ formats map[string]Format }

func NewRegistry() *Registry { return &Registry{formats: make(map[string]Format)} }

func DefaultRegistry() *Registry {
	r := NewRegistry()
	_ = r.Register(Format{
		Descriptor: Descriptor{ID: "110USextend", Name: "110USextend", Operations: []Operation{ReplaceFile, MergeList, MergeStrings}},
		Writer:     Extend110Writer{},
	})
	return r
}

func (r *Registry) Register(f Format) error {
	if f.Descriptor.ID == "" || (f.Reader == nil && f.Writer == nil) {
		return fmt.Errorf("mod 格式需要 ID 和读取或写入能力")
	}
	if _, ok := r.formats[f.Descriptor.ID]; ok {
		return fmt.Errorf("mod 格式已注册: %s", f.Descriptor.ID)
	}
	f.Descriptor.CanRead, f.Descriptor.CanWrite = f.Reader != nil, f.Writer != nil
	f.Descriptor.Operations = append([]Operation(nil), f.Descriptor.Operations...)
	r.formats[f.Descriptor.ID] = f
	return nil
}

func (r *Registry) Get(id string) (Format, error) {
	f, ok := r.formats[id]
	if !ok {
		return Format{}, fmt.Errorf("未知 mod 格式: %s", id)
	}
	f.Descriptor.Operations = append([]Operation(nil), f.Descriptor.Operations...)
	return f, nil
}

func (r *Registry) List() []Descriptor {
	result := make([]Descriptor, 0, len(r.formats))
	for id := range r.formats {
		f, _ := r.Get(id)
		result = append(result, f.Descriptor)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
