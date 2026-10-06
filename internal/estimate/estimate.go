// Package estimate 提供传输体积估算。
//
// 估算全程流式进行：内容经由 TeeReader 边读边压缩边计数，
// 任何时刻都不会把整份内容载入内存。
package estimate

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
)

// Algorithm 是压缩算法标识。
type Algorithm string

const (
	AlgNone    Algorithm = "none"    // 不压缩，Compressed == Raw
	AlgGzip    Algorithm = "gzip"    // gzip 容器 + deflate
	AlgDeflate Algorithm = "deflate" // 裸 deflate 流
)

// Config 配置压缩算法与档位。
// Level 为 0 表示使用算法默认档位；否则取 1(最快)..9(最佳压缩)。
type Config struct {
	Algorithm Algorithm `json:"algorithm"`
	Level     int       `json:"level"`
}

// Result 是估算结果。
type Result struct {
	Raw        int64     `json:"raw"`
	Compressed int64     `json:"compressed"`
	Algorithm  Algorithm `json:"algorithm"`
	Level      int       `json:"level"` // 实际生效的档位
}

// counter 是计数字节的 io.Writer。
type counter struct{ n *int64 }

func (c counter) Write(p []byte) (int, error) {
	*c.n += int64(len(p))
	return len(p), nil
}

// normalize 校验并规范化配置，返回实际生效的压缩级别。
func (cfg Config) normalize() (int, error) {
	switch cfg.Algorithm {
	case AlgNone, AlgGzip, AlgDeflate:
	case "":
		return 0, fmt.Errorf("estimate: algorithm is required")
	default:
		return 0, fmt.Errorf("estimate: unknown algorithm %q", cfg.Algorithm)
	}
	if cfg.Algorithm == AlgNone {
		return 0, nil
	}
	if cfg.Level == 0 {
		return gzip.DefaultCompression, nil
	}
	if cfg.Level < gzip.HuffmanOnly || cfg.Level > gzip.BestCompression {
		return 0, fmt.Errorf("estimate: invalid level %d (want %d..%d, or 0 for default)",
			cfg.Level, gzip.HuffmanOnly, gzip.BestCompression)
	}
	return cfg.Level, nil
}

// Estimate 流式估算 r 的传输体积：Raw 为压缩前字节数，Compressed 为压缩后字节数。
// 相同输入与配置下结果确定。
func Estimate(r io.Reader, cfg Config) (Result, error) {
	level, err := cfg.normalize()
	if err != nil {
		return Result{}, err
	}
	res := Result{Algorithm: cfg.Algorithm, Level: level}

	var raw int64
	src := io.TeeReader(r, counter{&raw})

	var compressed int64
	sink := counter{&compressed}

	switch cfg.Algorithm {
	case AlgNone:
		if _, err := io.Copy(sink, src); err != nil {
			return Result{}, fmt.Errorf("estimate: read: %w", err)
		}
	case AlgGzip:
		zw, err := gzip.NewWriterLevel(sink, level)
		if err != nil {
			return Result{}, fmt.Errorf("estimate: gzip: %w", err)
		}
		if _, err := io.Copy(zw, src); err != nil {
			return Result{}, fmt.Errorf("estimate: read: %w", err)
		}
		if err := zw.Close(); err != nil {
			return Result{}, fmt.Errorf("estimate: gzip close: %w", err)
		}
	case AlgDeflate:
		zw, err := flate.NewWriter(sink, level)
		if err != nil {
			return Result{}, fmt.Errorf("estimate: deflate: %w", err)
		}
		if _, err := io.Copy(zw, src); err != nil {
			return Result{}, fmt.Errorf("estimate: read: %w", err)
		}
		if err := zw.Close(); err != nil {
			return Result{}, fmt.Errorf("estimate: deflate close: %w", err)
		}
	}
	res.Raw = raw
	res.Compressed = compressed
	return res, nil
}
