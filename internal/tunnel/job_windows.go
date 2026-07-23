package tunnel

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// killOnCloseJob creates a Windows Job Object configured so that closing its
// handle terminates every process assigned to it (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE).
// The caller keeps the handle open for the tunnel's lifetime; closing it — whether
// explicitly via Close() or implicitly when this process dies — kills wireproxy.
func killOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// assignPID opens the given process id and assigns it to the job. PROCESS_SET_QUOTA
// and PROCESS_TERMINATE are the access rights AssignProcessToJobObject requires.
func assignPID(job windows.Handle, pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(job, h)
}
