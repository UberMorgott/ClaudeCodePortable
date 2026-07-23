// Package tunnel manages the wireproxy (AmneziaWG userspace proxy) child process.
//
// The crux is a Windows Job Object with KILL_ON_JOB_CLOSE: wireproxy is assigned to
// a job owned by this process, so it is guaranteed to die when this process exits —
// even on a hard/unclean parent kill — leaving no orphan bound to the proxy port.
package tunnel

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// Tunnel owns a running wireproxy process and the job object that guarantees its
// cleanup. Close it when done.
type Tunnel struct {
	job     windows.Handle
	proc    *os.Process
	pidFile string
	closed  bool
}

func noWindow() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// Validate runs `wireproxy-awg.exe -n -c proxyConf` (dry-run config check). A
// non-zero exit means the config was rejected.
func Validate(exe, proxyConf string) error {
	cmd := exec.Command(exe, "-n", "-c", proxyConf)
	cmd.SysProcAttr = noWindow()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wireproxy config rejected: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Start spawns wireproxy detached with no console window, assigns it to a
// KILL_ON_JOB_CLOSE job owned by this process, and writes its PID to pidFile.
// The returned Tunnel must be Close()d on exit; if the process is hard-killed
// without Close, the OS still tears down wireproxy when the job handle is released.
func Start(exe, proxyConf, pidFile string) (*Tunnel, error) {
	job, err := killOnCloseJob()
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	cmd := exec.Command(exe, "-s", "-c", proxyConf)
	cmd.SysProcAttr = noWindow()
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("start wireproxy: %w", err)
	}
	if err := assignPID(job, cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		windows.CloseHandle(job)
		return nil, fmt.Errorf("assign wireproxy to job: %w", err)
	}
	// Reap the child in the background so it never lingers as a zombie handle;
	// the job object owns its lifetime.
	go cmd.Wait()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		cmd.Process.Kill()
		windows.CloseHandle(job)
		return nil, fmt.Errorf("write pidfile: %w", err)
	}
	return &Tunnel{job: job, proc: cmd.Process, pidFile: pidFile}, nil
}

// Close closes the job handle (the OS then kills wireproxy) and removes the
// pidfile. Idempotent and safe on a nil receiver.
func (t *Tunnel) Close() error {
	if t == nil || t.closed {
		return nil
	}
	t.closed = true
	err := windows.CloseHandle(t.job)
	os.Remove(t.pidFile) // ignore absent
	return err
}

// KillStalePID reads a PID from pidFile and terminates that process, then removes
// the pidfile. No-op if the file is absent or the process is already gone — this
// heals an orphan left behind by a previous hard-killed session.
func KillStalePID(pidFile string) error {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if pid, convErr := strconv.Atoi(strings.TrimSpace(string(b))); convErr == nil && pid > 0 {
		if p, findErr := os.FindProcess(pid); findErr == nil {
			p.Kill() // ignore "already gone"
		}
	}
	os.Remove(pidFile)
	return nil
}

// PortInUse reports whether something is already LISTENING on bindAddr (e.g. a
// foreign process holding 127.0.0.1:25345). A successful dial => port is taken.
func PortInUse(bindAddr string) bool {
	conn, err := net.DialTimeout("tcp", bindAddr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
