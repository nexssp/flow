//go:build unix || windows

package cli

import (
	"os"
	"testing"
	"time"
)

func TestWaitForFileReadableTimeoutDataAndEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer reader.Close()
	defer writer.Close()

	ready, err := waitForFileReadable(reader, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("wait for silent pipe: %v", err)
	}
	if ready {
		t.Fatal("silent open pipe reported readable before its deadline")
	}

	if _, err := writer.WriteString("x"); err != nil {
		t.Fatalf("write pipe data: %v", err)
	}
	ready, err = waitForFileReadable(reader, time.Second)
	if err != nil || !ready {
		t.Fatalf("wait for pipe data = ready %v, error %v; want ready", ready, err)
	}
	buf := make([]byte, 1)
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("read pipe data: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	ready, err = waitForFileReadable(reader, time.Second)
	if err != nil || !ready {
		t.Fatalf("wait for pipe EOF = ready %v, error %v; want ready", ready, err)
	}
}
