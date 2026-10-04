package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"syscall"
	"time"
)

var helperName = regexp.MustCompile(`^Test[A-Za-z0-9]+$`)

var ErrClosing = errors.New("test child lifecycle closing")

// Child is a direct os.Executable test child, never a compiler/provider or
// arbitrary executable. Its concrete owner registers it before calling Start.
type Child struct {
	cmd                                                                     *exec.Cmd
	controlRead, controlWrite, eventRead, eventWrite, replyRead, replyWrite *endpoint
	done                                                                    chan struct{}
	mu                                                                      sync.Mutex
	startAttempted, hasProcess, confirmed, expectedKill                     bool
	waitErr                                                                 error
	output                                                                  output
	afterStart                                                              func()
	startDone                                                               chan struct{}
	closing                                                                 bool
	startErr                                                                error
}

// New returns a nonnil cleanup holder after any pipe allocation. No process is
// created yet; registration at the two concrete scopes must precede Start.
func New(ctx context.Context, helper, marker string) (*Child, error) {
	if ctx == nil || !helperName.MatchString(helper) || marker == "" {
		return nil, errors.New("invalid direct test child configuration")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 30*time.Second {
		return nil, errors.New("finite direct child context required")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, cause("test executable unavailable", err)
	}
	c := &Child{done: make(chan struct{}), startDone: make(chan struct{})}
	c.cmd = exec.CommandContext(ctx, executable, "-test.run=^"+helper+"$", "-test.count=1", "-test.timeout=30s")
	c.cmd.Env = append(os.Environ(), marker)
	c.cmd.Stdout, c.cmd.Stderr = &c.output, &c.output
	c.cmd.WaitDelay = time.Second
	pair := func(read, write **endpoint) error {
		r, w, e := os.Pipe()
		if e != nil {
			return e
		}
		*read, *write = &endpoint{File: r}, &endpoint{File: w}
		return nil
	}
	for _, p := range [][2]**endpoint{{&c.controlRead, &c.controlWrite}, {&c.eventRead, &c.eventWrite}, {&c.replyRead, &c.replyWrite}} {
		if err = pair(p[0], p[1]); err != nil {
			return c, cause("test child pipe allocation failed", err)
		}
	}
	c.cmd.Stdin = c.controlRead.File
	c.cmd.ExtraFiles = []*os.File{c.eventWrite.File, c.replyWrite.File}
	return c, nil
}

func (c *Child) Start() error {
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return ErrClosing
	}
	if c.startAttempted {
		c.mu.Unlock()
		return errors.New("test child Start repeated")
	}
	c.startAttempted = true
	c.mu.Unlock()
	defer close(c.startDone)
	err := c.cmd.Start()
	if c.afterStart != nil {
		c.afterStart()
	}
	if c.cmd.Process != nil {
		c.mu.Lock()
		c.hasProcess = true
		c.mu.Unlock()
		// This is the only Wait invocation; all borrowers select on its result.
		go func() {
			waitErr := c.cmd.Wait()
			c.mu.Lock()
			c.waitErr = waitErr
			c.confirmed = c.cmd.ProcessState != nil
			c.mu.Unlock()
			close(c.done)
		}()
	}
	var closed []error
	for _, p := range []*endpoint{c.controlRead, c.eventWrite, c.replyWrite} {
		if p != nil {
			closed = append(closed, cause("test child inherited parent endpoint close unknown", p.Close()))
		}
	}
	result := errors.Join(cause("test child Start failed", err), errors.Join(closed...))
	c.mu.Lock()
	c.startErr = result
	c.mu.Unlock()
	return result
}

func (c *Child) awaitStart(ctx context.Context) error {
	select {
	case <-c.startDone:
		return nil
	case <-ctx.Done():
		return cause("test child Start completion unknown", ctx.Err())
	}
}

func (c *Child) Send(ctx context.Context, value any) error {
	return WriteFrame(ctx, c.controlWrite, value)
}
func (c *Child) Event(ctx context.Context, value any) error {
	return ReadFrame(ctx, c.eventRead, value)
}
func (c *Child) Reply(ctx context.Context, value any) error {
	return ReadFrame(ctx, c.replyRead, value)
}

// Wait returns confirmation separately from the historical child exit cause.
// An error/timeout alone never grants permission to drop a borrowed scope.
func (c *Child) Wait(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, errors.New("child Wait context required")
	}
	if _, ok := ctx.Deadline(); !ok {
		return false, errors.New("finite child Wait context required")
	}
	c.mu.Lock()
	attempted := c.startAttempted
	c.mu.Unlock()
	if !attempted {
		return false, errors.New("child has not attempted Start")
	}
	if err := c.awaitStart(ctx); err != nil {
		return false, err
	}
	c.mu.Lock()
	hasProcess, startErr := c.hasProcess, c.startErr
	c.mu.Unlock()
	if !hasProcess {
		return true, startErr
	} // Actual Start completed with no OS handle.
	select {
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.confirmed {
			return false, errors.Join(errors.New("child Wait exit unconfirmed"), cause("child Wait failed", c.waitErr))
		}
		return true, cause("child exited with error", c.waitErr)
	case <-ctx.Done():
		return false, cause("finite child Wait unconfirmed", ctx.Err())
	}
}

func killed(err error) bool {
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		return false
	}
	status, ok := exited.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}

// KillWait validates the actual SIGKILL WaitStatus, not just any nonzero exit.
func (c *Child) KillWait(ctx context.Context) error {
	if ctx == nil {
		return errors.New("child SIGKILL context required")
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("finite child SIGKILL context required")
	}
	if err := c.awaitStart(ctx); err != nil {
		return err
	}
	if c.cmd.Process == nil {
		return errors.New("child SIGKILL requires actual process")
	}
	err := c.cmd.Process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return cause("child SIGKILL failed", err)
	}
	confirmed, waitErr := c.Wait(ctx)
	if !confirmed || !killed(waitErr) {
		return errors.Join(errors.New("expected SIGKILL exit not confirmed"), waitErr)
	}
	c.mu.Lock()
	c.expectedKill = true
	c.mu.Unlock()
	return nil
}

func (c *Child) closePipes() error {
	var errs []error
	for _, p := range []*endpoint{c.controlRead, c.controlWrite, c.eventRead, c.eventWrite, c.replyRead, c.replyWrite} {
		if p != nil {
			errs = append(errs, cause("child physical pipe close unknown", p.Close()))
		}
	}
	return errors.Join(errs...)
}

// Stop reports complete physical cleanup separately from historical exit
// errors. Unknown Wait or first pipe Close reports false, holding both concrete
// scopes. Parent native DB unknown remains independent of child exit.
func (c *Child) Stop(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, errors.New("child Stop context required")
	}
	if _, ok := ctx.Deadline(); !ok {
		return false, errors.New("finite child Stop context required")
	}
	c.mu.Lock()
	c.closing = true
	attempted := c.startAttempted
	c.mu.Unlock()
	if attempted {
		if err := c.awaitStart(ctx); err != nil {
			return false, err
		}
	}
	c.mu.Lock()
	hasProcess, confirmed, expected := c.hasProcess, c.confirmed, c.expectedKill
	startErr := c.startErr
	c.mu.Unlock()
	if !hasProcess {
		err := c.closePipes()
		return err == nil, errors.Join(startErr, err)
	}
	var killErr error
	sentKill := false
	if !confirmed {
		killErr = c.cmd.Process.Kill()
		sentKill = killErr == nil
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
	}
	confirmed, waitErr := c.Wait(ctx)
	if confirmed && (expected || sentKill && killed(waitErr)) {
		c.mu.Lock()
		c.expectedKill = true
		c.mu.Unlock()
		waitErr = nil
	}
	pipeErr := c.closePipes()
	return confirmed && pipeErr == nil, errors.Join(cause("child cleanup Kill failed", killErr), waitErr, pipeErr)
}

type output struct {
	mu   sync.Mutex
	data []byte
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(p)
	if room := 8192 - len(o.data); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		o.data = append(o.data, p...)
	}
	return n, nil
}

// Inherited holds exactly the child's control/event/reply FDs. Nonblocking
// poller registration makes physical cancellation effective as in02.
type Inherited struct{ control, events, reply *endpoint }

func OpenInherited() (*Inherited, error) {
	p := &Inherited{}
	for i, fd := range []int{0, 3, 4} {
		if err := syscall.SetNonblock(fd, true); err != nil {
			return p, cause("child inherited pipe unavailable", err)
		}
		file := &endpoint{File: os.NewFile(uintptr(fd), "test-process-pipe")}
		switch i {
		case 0:
			p.control = file
		case 1:
			p.events = file
		case 2:
			p.reply = file
		}
	}
	return p, nil
}
func (p *Inherited) Receive(ctx context.Context, value any) error {
	return ReadFrame(ctx, p.control, value)
}
func (p *Inherited) Emit(ctx context.Context, value any) error {
	return WriteFrame(ctx, p.events, value)
}
func (p *Inherited) Respond(ctx context.Context, value any) error {
	return WriteFrame(ctx, p.reply, value)
}
func (p *Inherited) Close() error {
	var errs []error
	for _, f := range []*endpoint{p.control, p.events, p.reply} {
		if f != nil {
			errs = append(errs, cause("child inherited pipe close unknown", f.Close()))
		}
	}
	return errors.Join(errs...)
}

var _ io.ReadWriteCloser = (*endpoint)(nil)
