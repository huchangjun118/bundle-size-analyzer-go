package analyzer

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func TestEstimateTransferNone(t *testing.T) {
	data := strings.Repeat("hello world ", 1000)
	est, err := EstimateTransfer("a.js", strings.NewReader(data), TransferConfig{Algorithm: CompressionNone})
	if err != nil {
		t.Fatal(err)
	}
	if est.RawBytes != int64(len(data)) || est.TransferBytes != int64(len(data)) {
		t.Fatalf("est = %+v, want raw=transfer=%d", est, len(data))
	}
}

func TestEstimateTransferGzipMatchesStdlib(t *testing.T) {
	data := strings.Repeat("compressible content ", 5000)
	est, err := EstimateTransfer("a.js", strings.NewReader(data), TransferConfig{Algorithm: CompressionGzip})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if est.RawBytes != int64(len(data)) {
		t.Fatalf("raw = %d, want %d", est.RawBytes, len(data))
	}
	if est.TransferBytes != int64(buf.Len()) {
		t.Fatalf("transfer = %d, want %d", est.TransferBytes, buf.Len())
	}
	if est.TransferBytes >= est.RawBytes {
		t.Fatal("compressible data should shrink")
	}
}

func TestEstimateTransferLevelConfigurable(t *testing.T) {
	data := strings.Repeat("abcdefghij", 2000)
	best, err := EstimateTransfer("a.js", strings.NewReader(data), TransferConfig{Algorithm: CompressionGzip, Level: 9})
	if err != nil {
		t.Fatal(err)
	}
	fast, err := EstimateTransfer("a.js", strings.NewReader(data), TransferConfig{Algorithm: CompressionGzip, Level: 1})
	if err != nil {
		t.Fatal(err)
	}
	if best.TransferBytes > fast.TransferBytes {
		t.Fatalf("level 9 (%d) should not exceed level 1 (%d)", best.TransferBytes, fast.TransferBytes)
	}
	for _, alg := range []Compression{CompressionFlate, CompressionZlib} {
		if _, err := EstimateTransfer("a.js", strings.NewReader(data), TransferConfig{Algorithm: alg}); err != nil {
			t.Fatalf("%s: %v", alg, err)
		}
	}
}

// 估算必须流式进行：用 io.Pipe 边写边估，证明不需要整份内容预先就绪。
func TestEstimateTransferStreaming(t *testing.T) {
	pr, pw := io.Pipe()
	done := make(chan TransferEstimate, 1)
	go func() {
		est, err := EstimateTransfer("pipe.js", pr, TransferConfig{Algorithm: CompressionGzip})
		if err != nil {
			t.Error(err)
		}
		done <- est
	}()
	chunk := bytes.Repeat([]byte("x"), 4096)
	var total int64
	for i := 0; i < 100; i++ {
		if _, err := pw.Write(chunk); err != nil {
			t.Fatal(err)
		}
		total += int64(len(chunk))
	}
	pw.Close()
	est := <-done
	if est.RawBytes != total {
		t.Fatalf("raw = %d, want %d", est.RawBytes, total)
	}
}

func TestEstimateTransferUnknownAlgorithm(t *testing.T) {
	if _, err := EstimateTransfer("a.js", strings.NewReader("x"), TransferConfig{Algorithm: "brotli"}); err == nil {
		t.Fatal("want error for unknown algorithm")
	}
}
