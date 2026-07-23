package tunnel

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const stillActive = 259 // STILL_ACTIVE

// pidAlive reports whether a process with the given PID currently exists and is
// still running (exit code STILL_ACTIVE).
func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false // cannot open => gone (or reused; unlikely in test window)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// TestMain doubles as a re-exec helper. When TUNNEL_JOB_PIDFILE is set the binary
// runs as the "job owner": it creates a KILL_ON_JOB_CLOSE job, spawns a long-lived
// child (ping), assigns it, records the child PID, then exits WITHOUT closing the
// job — proving the OS kills the child purely because the owner process died.
func TestMain(m *testing.M) {
	if pf := os.Getenv("TUNNEL_JOB_PIDFILE"); pf != "" {
		os.Exit(runJobHelper(pf))
	}
	os.Exit(m.Run())
}

func runJobHelper(pidFile string) int {
	job, err := killOnCloseJob()
	if err != nil {
		return 10
	}
	child := exec.Command("cmd", "/c", "ping", "-n", "999", "127.0.0.1")
	child.SysProcAttr = noWindow()
	if err := child.Start(); err != nil {
		return 11
	}
	if err := assignPID(job, child.Process.Pid); err != nil {
		child.Process.Kill()
		return 12
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		child.Process.Kill()
		return 13
	}
	// Exit without CloseHandle(job) and without killing the child: process exit
	// releases the job handle, and KILL_ON_JOB_CLOSE must reap the child.
	return 0
}

func TestJobKillOnClose(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	helper := exec.Command(os.Args[0])
	helper.Env = append(os.Environ(), "TUNNEL_JOB_PIDFILE="+pidFile)
	if out, err := helper.CombinedOutput(); err != nil {
		t.Fatalf("job helper failed: %v: %s", err, out)
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("helper did not record child pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("bad pid %q: %v", b, err)
	}
	// The kill is asynchronous with job-handle release; poll briefly.
	for i := 0; i < 50; i++ {
		if !pidAlive(pid) {
			return // success: child died when the owner exited
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Cleanup so a failed run doesn't leak a ping.
	if p, e := os.FindProcess(pid); e == nil {
		p.Kill()
	}
	t.Fatalf("child pid %d still alive after owner exit — KILL_ON_JOB_CLOSE did not fire", pid)
}

func TestKillStalePID_KillsRunning(t *testing.T) {
	child := exec.Command("cmd", "/c", "ping", "-n", "999", "127.0.0.1")
	child.SysProcAttr = noWindow()
	if err := child.Start(); err != nil {
		t.Fatalf("spawn child: %v", err)
	}
	pid := child.Process.Pid
	go child.Wait()

	pidFile := filepath.Join(t.TempDir(), "stale.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := KillStalePID(pidFile); err != nil {
		t.Fatalf("KillStalePID: %v", err)
	}
	var alive bool
	for i := 0; i < 30; i++ {
		if !pidAlive(pid) {
			break
		}
		alive = true
		time.Sleep(100 * time.Millisecond)
	}
	if pidAlive(pid) {
		child.Process.Kill()
		t.Fatalf("pid %d still alive after KillStalePID (was alive during poll: %v)", pid, alive)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("pidfile not removed: stat err=%v", err)
	}
}

func TestKillStalePID_AbsentFile(t *testing.T) {
	if err := KillStalePID(filepath.Join(t.TempDir(), "does-not-exist.pid")); err != nil {
		t.Fatalf("absent pidfile should be a no-op, got: %v", err)
	}
}

func TestPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if !PortInUse(addr) {
		ln.Close()
		t.Fatalf("PortInUse(%s) = false while a listener is open", addr)
	}
	ln.Close()
	// After close the port is free; expect false. (Small reuse race is acceptable
	// for a hermetic test — the OS holds the port briefly in TIME_WAIT-ish state
	// only for connected sockets, not an unaccepted listener.)
	if PortInUse(addr) {
		t.Fatalf("PortInUse(%s) = true after listener closed", addr)
	}
}
