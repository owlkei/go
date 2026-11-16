//go:build unix

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestLoadDotEnv_NotRegularFile_FIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.env")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skip("cannot create a FIFO:", err)
	}

	done := make(chan string, 1)
	go func() {
		text, _ := panicText(func() {
			Register(&ConfigModuleOptions{ENVFilePaths: []string{path}})
		})
		done <- text
	}()

	select {
	case text := <-done:
		want := "invalid config: " + path + ": not a regular file"
		if text != want {
			t.Error(test.DiffMessage(text, want, "E7/E9: a FIFO is refused"))
		}
	case <-time.After(30 * time.Second):
		if f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
		t.Fatal(test.DiffMessage("blocked", "a panic", "E7: Register must not block on a FIFO"))
	}
}

func TestLoadDotEnv_NotRegularFile_Device(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("no /dev/zero")
	}
	text, panicked := panicText(func() {
		Register(&ConfigModuleOptions{ENVFilePaths: []string{"/dev/zero"}})
	})
	want := "invalid config: /dev/zero: not a regular file"
	if !panicked || text != want {
		t.Error(test.DiffMessage(text, want, "E7/E9: /dev/zero is refused, never read"))
	}
}
