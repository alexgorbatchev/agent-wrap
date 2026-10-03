package activity

import (
	"context"
	"slices"

	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-watcher/scanner"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

// OwnedPID restricts discovery to the PTY session created by the frame.
func OwnedPID(pid, leader int) bool {
	if pid <= 0 || leader <= 0 {
		return false
	}
	sid, err := unix.Getsid(pid)
	return err == nil && sid == leader
}

// ScopedProvider restricts process discovery to the child's native PTY session.
func ScopedProvider(leader int, harness watcher.Harness) scanner.ProcessSnapshotProvider {
	return scanner.ProcessSnapshotProviderFunc(func(ctx context.Context) (scanner.ProcessSnapshot, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s, err := scanner.CaptureProcessSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		scoped := scopedSnapshot{snapshot: s, leader: leader, harness: harness}
		if harness == watcher.HarnessPi || harness == watcher.HarnessCodex || harness == watcher.HarnessOpencode {
			meta, ok := s.Process(leader)
			if ok && meta.Alive && OwnedPID(leader, leader) {
				p, err := process.NewProcessWithContext(ctx, int32(leader))
				if err != nil {
					return nil, err
				}
				cwd, err := p.CwdWithContext(ctx)
				if err != nil {
					return nil, err
				}
				scoped.launch, scoped.cwd = meta, cwd
			}
		}
		return scoped, nil
	})
}

type scopedSnapshot struct {
	snapshot scanner.ProcessSnapshot
	leader   int
	harness  watcher.Harness
	launch   scanner.ProcessMeta
	cwd      string
}

func (s scopedSnapshot) IsProcessAlive(pid int) bool {
	return OwnedPID(pid, s.leader) && s.snapshot.IsProcessAlive(pid)
}
func (s scopedSnapshot) VerifyProcessMatch(pid int, start int64) bool {
	return OwnedPID(pid, s.leader) && s.snapshot.VerifyProcessMatch(pid, start)
}
func (s scopedSnapshot) Process(pid int) (scanner.ProcessMeta, bool) {
	if !OwnedPID(pid, s.leader) {
		return scanner.ProcessMeta{}, false
	}
	return s.snapshot.Process(pid)
}
func (s scopedSnapshot) PiProcesses() []scanner.PiProcessInfo {
	procs := slices.DeleteFunc(slices.Clone(s.snapshot.PiProcesses()), func(p scanner.PiProcessInfo) bool { return !OwnedPID(p.PID, s.leader) })
	if s.harness == watcher.HarnessPi && s.launch.PID > 0 && !slices.ContainsFunc(procs, func(p scanner.PiProcessInfo) bool { return p.PID == s.launch.PID }) {
		procs = append(procs, scanner.PiProcessInfo{PID: s.launch.PID, CWD: s.cwd, CreateTime: s.launch.CreateTime})
	}
	return procs
}
func (s scopedSnapshot) CodexProcesses() []scanner.CodexProcessInfo {
	procs := slices.DeleteFunc(slices.Clone(s.snapshot.CodexProcesses()), func(p scanner.CodexProcessInfo) bool { return !OwnedPID(p.PID, s.leader) })
	if s.harness == watcher.HarnessCodex && s.launch.PID > 0 && !slices.ContainsFunc(procs, func(p scanner.CodexProcessInfo) bool { return p.PID == s.launch.PID }) {
		procs = append(procs, scanner.CodexProcessInfo{PID: s.launch.PID, CWD: s.cwd, CreateTime: s.launch.CreateTime})
	}
	return procs
}
func (s scopedSnapshot) OpencodeProcesses() []scanner.OpencodeProcessInfo {
	procs := slices.DeleteFunc(slices.Clone(s.snapshot.OpencodeProcesses()), func(p scanner.OpencodeProcessInfo) bool { return !OwnedPID(p.PID, s.leader) })
	if s.harness == watcher.HarnessOpencode && s.launch.PID > 0 && !slices.ContainsFunc(procs, func(p scanner.OpencodeProcessInfo) bool { return p.PID == s.launch.PID }) {
		procs = append(procs, scanner.OpencodeProcessInfo{PID: s.launch.PID, CWD: s.cwd, CreateTime: s.launch.CreateTime})
	}
	return procs
}
