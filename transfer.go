package analyzer

import (
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
)

// Compression 压缩算法标识。
type Compression string

const (
	CompressionNone  Compression = "none"
	CompressionGzip  Compression = "gzip"
	CompressionFlate Compression = "flate"
	CompressionZlib  Compression = "zlib"
)

// TransferConfig 配置一次传输体积估算。
type TransferConfig struct {
	Algorithm Compression
	Level     int // 压缩档位，语义随算法而定；0 表示算法默认
}

// TransferEstimate 是单个输入的估算结果。
type TransferEstimate struct {
	Name          string
	RawBytes      int64
	TransferBytes int64 // 压缩后（或未压缩时的原始）字节数
}

// countingWriter 只统计写入字节数，不保存内容。
type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// EstimateTransfer 以流式方式估算 r 的传输体积：
// 数据经由固定大小缓冲从 r 流向压缩器再到计数器，
// 任何时刻内存占用与输入总大小无关。
func EstimateTransfer(name string, r io.Reader, cfg TransferConfig) (TransferEstimate, error) {
	est := TransferEstimate{Name: name}
	raw := &countingWriter{}
	compressed := &countingWriter{}

	// tee：读出的内容同时计入原始字节数。
	src := io.TeeReader(r, raw)

	switch cfg.Algorithm {
	case CompressionNone, "":
		if _, err := io.Copy(compressed, src); err != nil {
			return est, fmt.Errorf("estimate %s: %w", name, err)
		}
	case CompressionGzip, CompressionFlate, CompressionZlib:
		w, err := newCompressor(compressed, cfg)
		if err != nil {
			return est, err
		}
		if _, err := io.Copy(w, src); err != nil {
			w.Close()
			return est, fmt.Errorf("estimate %s: %w", name, err)
		}
		if err := w.Close(); err != nil {
			return est, fmt.Errorf("estimate %s: %w", name, err)
		}
	default:
		return est, fmt.Errorf("estimate %s: unknown compression %q", name, cfg.Algorithm)
	}

	est.RawBytes = raw.n
	est.TransferBytes = compressed.n
	return est, nil
}

func newCompressor(dst io.Writer, cfg TransferConfig) (io.WriteCloser, error) {
	level := cfg.Level
	if level == 0 {
		level = flate.DefaultCompression
	}
	switch cfg.Algorithm {
	case CompressionGzip:
		return gzip.NewWriterLevel(dst, level)
	case CompressionFlate:
		return flate.NewWriter(dst, level)
	case CompressionZlib:
		return zlib.NewWriterLevel(dst, level)
	}
	return nil, fmt.Errorf("unknown compression %q", cfg.Algorithm)
}
