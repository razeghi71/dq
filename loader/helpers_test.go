package loader

import (
	"bytes"
	"compress/gzip"
	"testing"
)

// BoolPtr returns a pointer to b (test helper).
func BoolPtr(b bool) *bool {
	v := b
	return &v
}

func gzipTestBytes(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
