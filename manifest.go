package analyzer

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

// Manifest 描述一份构建产物清单。
// 兼容 v1 与 v2 两种格式；v2 新增字段不影响已有字段的归因。
type Manifest struct {
	Version int
	Chunks  []Chunk
	Modules []Module
}

// Chunk 是一个资源块（构建产物中的一个输出文件）。
type Chunk struct {
	ID      string // 资源块标识
	File    string // 产物文件路径（相对或绝对）
	Size    int64  // 产物文件在磁盘上的实际字节数（占用量）
	Kind    string // v2 新增：资源类别（如 "js"、"css"），未知时为空
	Entries []string
}

// Module 是一个源码模块，模块之间通过 Deps 互相引用。
type Module struct {
	ID   string
	Size int64
	Deps []string
	Dir  string // v2 新增：显式目录，缺省时由 ID 推导
}

// manifestJSON 同时覆盖 v1 与 v2 的字段。
// v1 没有 version/kind/dir 字段；v2 新增字段在解码时被忽略则不影响 v1，
// 反过来 v1 输入缺少新字段时取零值，保证已有字段的归因结果不变。
// 未知的未来字段由 encoding/json 默认行为静默忽略。
type manifestJSON struct {
	Version int `json:"version"`
	Chunks  []struct {
		ID      string   `json:"id"`
		File    string   `json:"file"`
		Size    int64    `json:"size"`
		Kind    string   `json:"kind"` // v2+
		Entries []string `json:"entries"`
	} `json:"chunks"`
	Modules []struct {
		ID   string   `json:"id"`
		Size int64    `json:"size"`
		Deps []string `json:"deps"`
		Dir  string   `json:"dir"` // v2+
	} `json:"modules"`
}

// ParseManifest 从 r 解析清单，自动识别 v1/v2 格式，未知字段被忽略。
func ParseManifest(r io.Reader) (*Manifest, error) {
	var raw manifestJSON
	dec := json.NewDecoder(r)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	version := raw.Version
	if version == 0 {
		version = 1 // 缺省视为 v1
	}
	if version < 1 || version > 2 {
		return nil, fmt.Errorf("parse manifest: unsupported version %d", version)
	}
	m := &Manifest{Version: version}
	for _, c := range raw.Chunks {
		if c.ID == "" {
			return nil, fmt.Errorf("parse manifest: chunk with empty id")
		}
		m.Chunks = append(m.Chunks, Chunk{
			ID:      c.ID,
			File:    c.File,
			Size:    c.Size,
			Kind:    c.Kind,
			Entries: append([]string(nil), c.Entries...),
		})
	}
	for _, mod := range raw.Modules {
		if mod.ID == "" {
			return nil, fmt.Errorf("parse manifest: module with empty id")
		}
		m.Modules = append(m.Modules, Module{
			ID:   mod.ID,
			Size: mod.Size,
			Deps: append([]string(nil), mod.Deps...),
			Dir:  mod.Dir,
		})
	}
	return m, nil
}

// moduleDir 返回模块所属目录：优先显式 Dir，否则由模块 ID 推导。
func moduleDir(mod Module) string {
	if mod.Dir != "" {
		return mod.Dir
	}
	d := path.Dir(mod.ID)
	if d == "." {
		return ""
	}
	return d
}

// chunkKind 返回资源块类别：优先显式 Kind，否则由文件扩展名推导。
func chunkKind(c Chunk) string {
	if c.Kind != "" {
		return c.Kind
	}
	ext := path.Ext(c.File)
	return strings.TrimPrefix(ext, ".")
}
