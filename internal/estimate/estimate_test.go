package estimate

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func TestEstimateNone(t *testing.T) {
	data := strings.Repeat("hello world ", 1000)
	res, err := Estimate(strings.NewReader(data), Config{Algorithm: AlgNone})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if res.Raw != int64(len(data)) || res.Compressed != res.Raw {
		t.Fatalf("none: raw=%d compressed=%d, want both %d", res.Raw, res.Compressed, len(data))
	}
}

func TestEstimateGzipMatchesRealEncoding(t *testing.T) {
	data := strings.Repeat("compressible payload ", 500)
	res, err := Estimate(strings.NewReader(data), Config{Algorithm: AlgGzip, Level: 6})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if res.Raw != int64(len(data)) {
		t.Fatalf("raw = %d, want %d", res.Raw, len(data))
	}
	// 与真实 gzip 编码结果逐字节对比大小
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, 6)
	zw.Write([]byte(data))
	zw.Close()
	if res.Compressed != int64(buf.Len()) {
		t.Fatalf("compressed = %d, want %d", res.Compressed, buf.Len())
	}
	if res.Compressed >= res.Raw {
		t.Fatal("expected compression to shrink repetitive data")
	}
}

func TestEstimateLevelsDiffer(t *testing.T) {
	data := strings.Repeat("abcdefgh", 4096)
	fast, err := Estimate(strings.NewReader(data), Config{Algorithm: AlgGzip, Level: 1})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	best, err := Estimate(strings.NewReader(data), Config{Algorithm: AlgGzip, Level: 9})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if best.Compressed > fast.Compressed {
		t.Fatalf("level 9 (%d) should be <= level 1 (%d)", best.Compressed, fast.Compressed)
	}
}

func TestEstimateDeflate(t *testing.T) {
	data := strings.Repeat("x", 10000)
	res, err := Estimate(strings.NewReader(data), Config{Algorithm: AlgDeflate})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if res.Level == 0 {
		t.Fatal("default level not resolved")
	}
	if res.Compressed >= res.Raw {
		t.Fatal("deflate should compress repetitive data")
	}
}

func TestEstimateInvalidConfig(t *testing.T) {
	if _, err := Estimate(strings.NewReader("x"), Config{}); err == nil {
		t.Fatal("want error for missing algorithm")
	}
	if _, err := Estimate(strings.NewReader("x"), Config{Algorithm: "zip"}); err == nil {
		t.Fatal("want error for unknown algorithm")
	}
	if _, err := Estimate(strings.NewReader("x"), Config{Algorithm: AlgGzip, Level: 42}); err == nil {
		t.Fatal("want error for invalid level")
	}
}

// 验证流式：reader 只被顺序读取，不要求可 Seek，也不整体入内存。
func TestEstimateStreamingSinglePass(t *testing.T) {
	reads := 0
	r := &limitedCounter{r: strings.NewReader(strings.Repeat("y", 1<<20)), reads: &reads}
	res, err := Estimate(r, Config{Algorithm: AlgGzip})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if res.Raw != 1<<20 {
		t.Fatalf("raw = %d, want %d", res.Raw, 1<<20)
	}
}

type limitedCounter struct {
	r     io.Reader
	reads *int
}

func (l *limitedCounter) Read(p []byte) (int, error) {
	*l.reads++
	return l.r.Read(p)
}

func TestEstimateDeterministic(t *testing.T) {
	data := strings.Repeat("deterministic ", 2000)
	a, _ := Estimate(strings.NewReader(data), Config{Algorithm: AlgGzip, Level: 5})
	b, _ := Estimate(strings.NewReader(data), Config{Algorithm: AlgGzip, Level: 5})
	if a != b {
		t.Fatalf("non-deterministic: %+v vs %+v", a, b)
	}
}
