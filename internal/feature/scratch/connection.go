package scratch

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type sharedWork struct {
	ctx            context.Context
	job            *poolJob
	extra1, script []byte
	worker         string
	connection     *liveConnection
}

type liveConnection struct {
	mu        sync.Mutex
	nonce     []byte
	exhausted bool
}

// Allocate a unique extranonce for each command, including concurrent commands.
func (c *liveConnection) nextNonce() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exhausted {
		return nil, fmt.Errorf("extranonce space exhausted")
	}
	n := append([]byte(nil), c.nonce...)
	for i := len(c.nonce) - 1; i >= 0; i-- {
		c.nonce[i]++
		if c.nonce[i] != 0 {
			return n, nil
		}
	}
	c.exhausted = true
	return n, nil
}

type candidate struct {
	ctx   context.Context
	work  *sharedWork
	extra []byte
	nonce uint32
	reply chan submissionResult
}
type submissionResult struct {
	status string
	err    error
}

type poolClient struct {
	mu          sync.Mutex
	work        *sharedWork
	changed     chan struct{}
	submissions chan candidate
	load        func() (string, []byte, error)
	dial        func(context.Context) (net.Conn, error)
	retry       time.Duration
}

func newPoolClient() *poolClient {
	return &poolClient{
		changed: make(chan struct{}), submissions: make(chan candidate), load: loadWallet, retry: time.Second,
		dial: func(ctx context.Context) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", poolAddress)
		},
	}
}

func (p *poolClient) publish(work *sharedWork) {
	p.mu.Lock()
	p.work = work
	close(p.changed)
	p.changed = make(chan struct{})
	p.mu.Unlock()
}

func (p *poolClient) serve(ctx context.Context, logger *log.Logger) {
	delay := p.retry
	for ctx.Err() == nil {
		started := time.Now()
		err := p.connect(ctx)
		p.publish(nil)
		if ctx.Err() != nil {
			return
		}
		logger.Printf("scratch connection: %v", err)
		if time.Since(started) > time.Minute {
			delay = p.retry
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}

func (p *poolClient) connect(ctx context.Context) error {
	address, script, err := p.load()
	if err != nil {
		return err
	}
	conn, err := p.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	s := &poolSession{conn: conn, events: make(chan poolEvent, 8)}
	go s.read(ctx)
	handshakeCtx, handshakeCancel := context.WithTimeout(ctx, poolTimeout)
	defer handshakeCancel()
	if err := conn.SetDeadline(time.Now().Add(poolTimeout)); err != nil {
		return err
	}
	if err := s.handshake(handshakeCtx, address); err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	handshakeCancel()
	live := &liveConnection{nonce: make([]byte, s.extra2Size)}
	if _, err := rand.Read(live.nonce); err != nil {
		return err
	}
	workCtx, invalidate := context.WithCancel(ctx)
	defer func() { invalidate() }()
	s.onJob = func(job *poolJob) error {
		// Verify every new job before making it available to commands.
		if _, _, err := job.header(s.extra1, make([]byte, s.extra2Size), script); err != nil {
			return fmt.Errorf("verify job: %w", err)
		}
		if job.clean {
			invalidate()
			workCtx, invalidate = context.WithCancel(ctx)
		}
		p.publish(&sharedWork{ctx: workCtx, job: job, extra1: s.extra1, script: script, worker: address + ".dicoco", connection: live})
		return nil
	}
	if s.job != nil {
		if err := s.onJob(s.job); err != nil {
			return err
		}
	}
	// A silent/dead connection must not leave an old job usable indefinitely.
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Minute)); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-s.events:
			if _, err := s.handle(e); err != nil {
				return err
			}
			if err := conn.SetReadDeadline(time.Now().Add(2 * time.Minute)); err != nil {
				return err
			}
		case request := <-p.submissions:
			if request.work.connection != live || request.work.ctx.Err() != nil || request.ctx.Err() != nil {
				request.reply <- submissionResult{"unknown", fmt.Errorf("job expired")}
				continue
			}
			deadline, _ := request.ctx.Deadline()
			if err := conn.SetWriteDeadline(deadline); err != nil {
				return err
			}
			status, err := s.submit(request.ctx, request.work.worker, request.work.job, request.extra, request.nonce)
			request.reply <- submissionResult{status, err}
			if err != nil {
				return err
			} // Reconnect; never confuse a late response with another submit.
			if err := conn.SetWriteDeadline(time.Time{}); err != nil {
				return err
			}
		}
	}
}

func (p *poolClient) acquire(ctx context.Context) (*sharedWork, error) {
	for {
		p.mu.Lock()
		work, changed := p.work, p.changed
		p.mu.Unlock()
		if work != nil && work.ctx.Err() == nil {
			return work, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for pool: %w", ctx.Err())
		case <-changed:
		}
	}
}

func (p *poolClient) run(attempts uint64) (Result, error) {
	if attempts == 0 || attempts > 1<<32 {
		return Result{}, fmt.Errorf("invalid attempt count")
	}
	ctx, cancel := context.WithTimeout(context.Background(), poolTimeout)
	defer cancel()
	preparing := time.Now()
	work, err := p.acquire(ctx)
	if err != nil {
		return Result{}, err
	}
	extra, err := work.connection.nextNonce()
	if err != nil {
		return Result{}, err
	}
	header, target, err := work.job.header(work.extra1, extra, work.script)
	if err != nil {
		return Result{}, err
	}
	started := time.Now()
	result := Result{ConnectElapsed: started.Sub(preparing)}
	for nonce := uint64(0); nonce < attempts; nonce++ {
		if nonce%128 == 0 {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if err := work.ctx.Err(); err != nil {
				return result, fmt.Errorf("job expired; retry command")
			}
		}
		binary.LittleEndian.PutUint32(header[76:], uint32(nonce))
		hash := doubleHash(header[:])
		if nonce == 0 || lessBitcoinHash(hash, result.BestHash) {
			result.BestHash = hash
		}
		result.Attempts = nonce + 1
		if !lessBitcoinHash(target, hash) {
			result.Score = 100
			result.Elapsed = time.Since(started)
			result.Submission = "unknown"
			submitStarted := time.Now()
			request := candidate{ctx: ctx, work: work, extra: extra, nonce: uint32(nonce), reply: make(chan submissionResult, 1)}
			select {
			case p.submissions <- request:
			case <-ctx.Done():
				return result, ctx.Err()
			case <-work.ctx.Done():
				return result, fmt.Errorf("job expired")
			}
			select {
			case response := <-request.reply:
				result.Submission = response.status
				result.SubmitElapsed = time.Since(submitStarted)
				return result, response.err
			case <-ctx.Done():
				return result, ctx.Err()
			}
		}
	}
	if work.ctx.Err() != nil {
		return result, fmt.Errorf("job expired; retry command")
	}
	result.Elapsed = time.Since(started)
	result.Score = scoreBestHash(result.BestHash, attempts)
	return result, nil
}
