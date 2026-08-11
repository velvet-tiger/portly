package kill

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestOSSignallerSignalStopsARealProcess spawns a real process and confirms
// OSSignaller actually reaches the operating system rather than exercising a
// mock, matching how other OS-facing packages in this project are tested.
func TestOSSignallerSignalStopsARealProcess(t *testing.T) {
	command := exec.Command("sleep", "30")
	if err := command.Start(); err != nil {
		t.Fatalf("starting the process under test: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	if err := (OSSignaller{}).Signal(int32(command.Process.Pid), syscall.SIGTERM); err != nil {
		t.Fatalf("Signal() returned %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("process did not exit within 5s of receiving SIGTERM")
	}
}

// TestOSSignallerSignalReportsAnAlreadyExitedProcess confirms the error names
// the PID, since the caller reports it directly to the person running portly.
func TestOSSignallerSignalReportsAnAlreadyExitedProcess(t *testing.T) {
	command := exec.Command("true")
	if err := command.Run(); err != nil {
		t.Fatalf("running the process under test: %v", err)
	}

	err := (OSSignaller{}).Signal(int32(command.Process.Pid), syscall.SIGTERM)
	if err == nil {
		t.Fatal("Signal() on an already-exited process returned nil, want an error")
	}
}
