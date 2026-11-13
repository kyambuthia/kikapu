package main

import (
	"flag"
	"testing"
	"time"
)

func TestLoadappDefaults(t *testing.T) {
	if concurrency != 20 {
		t.Errorf("default concurrency = %d, want 20", concurrency)
	}
	if duration != 10*time.Second {
		t.Errorf("default duration = %v, want 10s", duration)
	}
	if rpsCap != 0 {
		t.Errorf("default rpsCap = %d, want 0", rpsCap)
	}
	if flag.Lookup("scenario") == nil {
		t.Error("expected -scenario flag to be registered")
	}
}
