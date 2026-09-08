//go:build integration

package postgres_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/krav01/digital-goods-core/internal/delivery"
	"github.com/krav01/digital-goods-core/internal/httpapi"
	"github.com/krav01/digital-goods-core/internal/postgres"
	"github.com/krav01/digital-goods-core/internal/supplier"
)

type processEvent struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// TestProcessHelper runs the real stores, handlers and worker in a separate,
// race-instrumented test executable. Barriers exist only in this test adapter;
// production binaries have no crash-control environment variables or endpoints.
func TestProcessHelper(t *testing.T) {
	role := os.Getenv("DGC_HELPER_ROLE")
	if role == "" {
		t.Skip("subprocess entry point")
	}
	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	commands := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer cancel() // Parent death closes stdin and stops the child as well.
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			select {
			case commands <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() {
		cancel()
		if err := os.Stdin.Close(); err != nil {
			t.Error(err)
		}
		<-readerDone
	}()
	emit := func(e processEvent) {
		if err := json.NewEncoder(os.Stdout).Encode(e); err != nil {
			cancel()
		}
	}
	gate := func(ctx context.Context) error {
		select {
		case <-commands:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	if role == "idle" {
		emit(processEvent{Name: "ready"})
		<-ctx.Done()
		return
	}
	pool, err := postgres.Open(ctx, os.Getenv("DGC_HELPER_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := postgres.Ready(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if role == "worker" {
		store := &barrierStore{Store: postgres.New(pool), phase: os.Getenv("DGC_HELPER_PHASE"), gate: gate, emit: emit}
		emit(processEvent{Name: "ready"})
		if err := gate(ctx); err != nil {
			return
		}
		if err := delivery.NewWorker(store, supplier.NewClient(os.Getenv("DGC_HELPER_SUPPLIER")), logger).Run(ctx); err != nil {
			t.Fatal(err)
		}
		return
	}
	var handler http.Handler
	switch role {
	case "api":
		handler = httpapi.NewHandler(postgres.New(pool))
	case "supplier":
		handler = supplier.NewHandler(postgres.NewSupplier(pool))
	default:
		t.Fatalf("unknown helper role %q", role)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	emit(processEvent{Name: "ready", URL: "http://" + listener.Addr().String()})
	<-ctx.Done()
	if err := server.Close(); err != nil {
		t.Error(err)
	}
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		t.Error(err)
	}
}

type barrierStore struct {
	*postgres.Store
	phase   string
	gate    func(context.Context) error
	emit    func(processEvent)
	paused  bool
	claimed bool
}

// This harness regression runs without PostgreSQL, including in a local sandbox.
func TestProcessHarnessShutdown(t *testing.T) {
	p := startProcess(t, "idle", "", "", "")
	p.stop(t)
}

func (s *barrierStore) checkpoint(ctx context.Context, phase string) error {
	if s.phase != phase || s.paused {
		return nil
	}
	s.paused = true
	s.emit(processEvent{Name: "checkpoint"})
	return s.gate(ctx)
}

func (s *barrierStore) ProcessPayment(ctx context.Context) (bool, error) {
	worked, err := s.Store.ProcessPayment(ctx)
	if err == nil && worked {
		err = s.checkpoint(ctx, "after_payment")
	}
	return worked, err
}

func (s *barrierStore) ClaimDelivery(ctx context.Context, duration time.Duration) (delivery.Lease, bool, error) {
	lease, found, err := s.Store.ClaimDelivery(ctx, duration)
	if !s.claimed {
		s.claimed = true
		s.emit(processEvent{Name: "claim_checked"})
	}
	return lease, found, err
}

func (s *barrierStore) PrepareDelivery(ctx context.Context, lease delivery.Lease) (delivery.Request, error) {
	req, err := s.Store.PrepareDelivery(ctx, lease)
	if err == nil {
		err = s.checkpoint(ctx, "after_prepare")
	}
	return req, err
}

func (s *barrierStore) FinishDelivery(ctx context.Context, lease delivery.Lease, req delivery.Request, result delivery.Result, delay time.Duration) error {
	if err := s.checkpoint(ctx, "before_finish"); err != nil {
		return err
	}
	err := s.Store.FinishDelivery(ctx, lease, req, result, delay)
	if errors.Is(err, delivery.ErrLeaseLost) {
		s.emit(processEvent{Name: "lease_lost"})
	}
	return err
}

type lockedOutput struct {
	sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedOutput) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.buffer.Write(p)
}

func (b *lockedOutput) String() string {
	b.Lock()
	defer b.Unlock()
	return b.buffer.String()
}

type childProcess struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	events chan processEvent
	done   chan struct{}
	read   chan struct{}
	output lockedOutput
	err    error // Written before done closes; read only after receiving done.
	url    string
	ended  bool
}

func startProcess(t *testing.T, role, dsn, supplierURL, phase string) *childProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &childProcess{events: make(chan processEvent, 16), done: make(chan struct{}), read: make(chan struct{})}
	p.cmd = exec.Command(executable, "-test.run=^TestProcessHelper$", "-test.timeout=90s")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "DGC_HELPER_") {
			p.cmd.Env = append(p.cmd.Env, entry)
		}
	}
	p.cmd.Env = append(p.cmd.Env, "DGC_HELPER_ROLE="+role, "DGC_HELPER_DSN="+dsn,
		"DGC_HELPER_SUPPLIER="+supplierURL, "DGC_HELPER_PHASE="+phase)
	p.cmd.Stderr = &p.output
	p.input, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(p.read)
		defer close(p.events)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			var e processEvent
			if json.Unmarshal(scanner.Bytes(), &e) == nil && e.Name != "" {
				select {
				case p.events <- e:
				case <-p.done:
					return
				}
			} else if _, err := fmt.Fprintln(&p.output, scanner.Text()); err != nil {
				return
			}
		}
	}()
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		p.stop(t)
		<-p.read
		if t.Failed() {
			t.Logf("%s PID %d output:\n%s", role, p.cmd.Process.Pid, p.output.String())
		}
	})
	p.url = p.await(t, "ready").URL
	t.Logf("%s ready, PID %d", role, p.cmd.Process.Pid)
	return p
}

func (p *childProcess) await(t *testing.T, name string) processEvent {
	t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-p.events:
			if !ok {
				t.Fatalf("child closed output before %s: %s", name, p.output.String())
			}
			if e.Name == name {
				return e
			}
		case <-timer.C:
			t.Fatalf("child did not reach %s: %s", name, p.output.String())
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
}

func (p *childProcess) resume(t *testing.T) {
	t.Helper()
	if _, err := fmt.Fprintln(p.input, "continue"); err != nil {
		t.Fatal(err)
	}
}

func (p *childProcess) kill(t *testing.T) {
	t.Helper()
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
		var exit *exec.ExitError
		if !errors.As(p.err, &exit) {
			t.Fatalf("expected SIGKILL exit, got %v", p.err)
		}
		status, ok := exit.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("unexpected kill status: %v", exit)
		}
		p.ended = true
	case <-time.After(5 * time.Second):
		t.Fatal("killed child did not exit")
	}
}

func (p *childProcess) stop(t *testing.T) {
	t.Helper()
	if p.ended {
		return
	}
	select {
	case <-p.done:
		p.ended = true
		t.Errorf("child exited unexpectedly: %v; %s", p.err, p.output.String())
		return
	default:
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Error(err)
	}
	// SIGTERM cancels work but cannot unblock every inherited-stdin Read on Unix.
	// The parent owns the writer: EOF lets the child join its command reader.
	if err := p.input.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Error(err)
	}
	select {
	case <-p.done:
		p.ended = true
		if p.err != nil {
			t.Errorf("child failed: %v; %s", p.err, p.output.String())
		}
	case <-time.After(5 * time.Second):
		p.kill(t)
		t.Error("child required forced cleanup")
	}
}
