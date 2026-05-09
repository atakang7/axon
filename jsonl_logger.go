package main

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// jsonlLogger writes one JSON object per line.
type jsonlLogger struct {
	mu sync.Mutex
	w  io.WriteCloser
}

func newJSONLLogger(path string) (*jsonlLogger, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &jsonlLogger{w: f}, nil
}

func (l *jsonlLogger) Emit(kind string, fields map[string]any) {
	if l == nil {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
	fields["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	fields["kind"] = kind
	l.mu.Lock()
	defer l.mu.Unlock()
	enc := json.NewEncoder(l.w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(fields)
}

func (l *jsonlLogger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.w.Close()
}
