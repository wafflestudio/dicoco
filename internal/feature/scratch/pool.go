package scratch

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"time"
)

const poolAddress = "stratum.ckpool.org:3333"
const poolTimeout = 15 * time.Second

type poolMessage struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
	Result json.RawMessage   `json:"result"`
	Error  json.RawMessage   `json:"error"`
}

type poolEvent struct {
	message poolMessage
	err     error
}
type poolSession struct {
	onJob      func(*poolJob) error
	conn       net.Conn
	events     chan poolEvent
	job        *poolJob
	extra1     []byte
	extra2Size int
}
type poolJob struct {
	id, ntime                string
	prev, coin1, coin2       []byte
	branches                 [][]byte
	version, bits, timestamp uint32
	clean                    bool
}

func (s *poolSession) handshake(ctx context.Context, address string) error {
	if err := s.send(1, "mining.subscribe", []any{"dicoco/1.0"}); err != nil {
		return err
	}
	sub, err := s.wait(ctx, 1)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	var fields []json.RawMessage
	if json.Unmarshal(sub, &fields) != nil || len(fields) != 3 {
		return fmt.Errorf("invalid subscription")
	}
	var extra string
	if json.Unmarshal(fields[1], &extra) != nil || json.Unmarshal(fields[2], &s.extra2Size) != nil || s.extra2Size < 1 || s.extra2Size > 32 {
		return fmt.Errorf("invalid extranonce")
	}
	s.extra1, err = decodeHex(extra, -1, 32)
	if err != nil {
		return err
	}
	worker := address + ".dicoco"
	if err := s.send(2, "mining.authorize", []any{worker, "x"}); err != nil {
		return err
	}
	approved, err := s.wait(ctx, 2)
	if err != nil || string(approved) != "true" {
		return fmt.Errorf("authorization failed: %v", err)
	}
	return nil
}

func (s *poolSession) submit(ctx context.Context, worker string, job *poolJob, extra2 []byte, nonce uint32) (string, error) {
	params := []any{worker, job.id, hex.EncodeToString(extra2), job.ntime, fmt.Sprintf("%08x", nonce)}
	if err := s.send(3, "mining.submit", params); err != nil {
		return "unknown", fmt.Errorf("submit candidate: %w", err)
	}
	accepted, err := s.wait(ctx, 3)
	if err != nil {
		return "unknown", fmt.Errorf("candidate response: %w", err)
	}
	if string(accepted) != "true" {
		return "rejected", fmt.Errorf("candidate rejected")
	}
	return "accepted", nil
}

func (s *poolSession) read(ctx context.Context) {
	scanner := bufio.NewScanner(s.conn)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		var m poolMessage
		err := json.Unmarshal(scanner.Bytes(), &m)
		select {
		case s.events <- poolEvent{m, err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	select {
	case s.events <- poolEvent{err: err}:
	case <-ctx.Done():
	}
}

func (s *poolSession) send(id int, method string, params []any) error {
	return json.NewEncoder(s.conn).Encode(struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params []any  `json:"params"`
	}{id, method, params})
}

func (s *poolSession) handle(e poolEvent) (poolMessage, error) {
	if e.err != nil {
		return e.message, e.err
	}
	switch e.message.Method {
	case "mining.notify":
		job, err := parseJob(e.message.Params)
		if err != nil {
			return e.message, err
		}
		s.job = job
		if s.onJob != nil {
			return e.message, s.onJob(job)
		}
	case "mining.set_extranonce", "client.reconnect":
		return e.message, fmt.Errorf("pool changed session; retry command")
	}
	return e.message, nil
}

func (s *poolSession) next(ctx context.Context) (poolMessage, error) {
	select {
	case e := <-s.events:
		return s.handle(e)
	case <-ctx.Done():
		return poolMessage{}, ctx.Err()
	}
}

func (s *poolSession) wait(ctx context.Context, id int) (json.RawMessage, error) {
	for {
		m, err := s.next(ctx)
		if err != nil {
			return nil, err
		}
		if string(m.ID) != strconv.Itoa(id) {
			continue
		}
		if len(m.Error) != 0 && string(m.Error) != "null" {
			return nil, fmt.Errorf("pool rejected request %d", id)
		}
		return m.Result, nil
	}
}

func decodeHex(value string, size, max int) ([]byte, error) {
	if len(value) > max*2 || (size >= 0 && len(value) != size*2) {
		return nil, fmt.Errorf("invalid hex field length")
	}
	b, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid hex field")
	}
	return b, nil
}

func parseJob(p []json.RawMessage) (*poolJob, error) {
	if len(p) != 9 {
		return nil, fmt.Errorf("invalid job field count")
	}
	var text [8]string
	for _, i := range []int{0, 1, 2, 3, 5, 6, 7} {
		if json.Unmarshal(p[i], &text[i]) != nil {
			return nil, fmt.Errorf("invalid job field")
		}
	}
	if len(text[0]) == 0 || len(text[0]) > 128 {
		return nil, fmt.Errorf("invalid job id")
	}
	j := &poolJob{id: text[0], ntime: text[7]}
	var err error
	if j.prev, err = decodeHex(text[1], 32, 32); err != nil {
		return nil, err
	}
	if j.coin1, err = decodeHex(text[2], -1, 8000); err != nil {
		return nil, err
	}
	if j.coin2, err = decodeHex(text[3], -1, 8000); err != nil {
		return nil, err
	}
	var branches []string
	if json.Unmarshal(p[4], &branches) != nil || len(branches) > 32 {
		return nil, fmt.Errorf("invalid merkle branches")
	}
	for _, branch := range branches {
		b, err := decodeHex(branch, 32, 32)
		if err != nil {
			return nil, err
		}
		j.branches = append(j.branches, b)
	}
	for i, dst := range map[int]*uint32{5: &j.version, 6: &j.bits, 7: &j.timestamp} {
		b, err := decodeHex(text[i], 4, 4)
		if err != nil {
			return nil, err
		}
		*dst = binary.BigEndian.Uint32(b)
	}
	if json.Unmarshal(p[8], &j.clean) != nil {
		return nil, fmt.Errorf("invalid clean flag")
	}
	return j, nil
}

func (j *poolJob) header(extra1, extra2, script []byte) ([80]byte, [32]byte, error) {
	var header [80]byte
	target, err := compactTarget(j.bits)
	if err != nil {
		return header, target, err
	}
	coin := make([]byte, 0, len(j.coin1)+len(extra1)+len(extra2)+len(j.coin2))
	coin = append(coin, j.coin1...)
	coin = append(coin, extra1...)
	coin = append(coin, extra2...)
	coin = append(coin, j.coin2...)
	if err := validateCoinbase(coin, script); err != nil {
		return header, target, err
	}
	root := doubleHash(coin)
	for _, branch := range j.branches {
		var pair [64]byte
		copy(pair[:32], root[:])
		copy(pair[32:], branch)
		root = doubleHash(pair[:])
	}
	binary.LittleEndian.PutUint32(header[:4], j.version)
	// Stratum V1 prevhash is word-swapped relative to wire order.
	for i := 0; i < 32; i += 4 {
		for k := 0; k < 4; k++ {
			header[4+i+k] = j.prev[i+3-k]
		}
	}
	copy(header[36:68], root[:])
	binary.LittleEndian.PutUint32(header[68:72], j.timestamp)
	binary.LittleEndian.PutUint32(header[72:76], j.bits)
	return header, target, nil
}

func doubleHash(b []byte) [32]byte { first := sha256.Sum256(b); return sha256.Sum256(first[:]) }

func compactTarget(bits uint32) ([32]byte, error) {
	var target [32]byte
	exponent := bits >> 24
	mantissa := bits & 0x7fffff
	if bits&0x800000 != 0 || mantissa == 0 || exponent > 34 {
		return target, fmt.Errorf("invalid target")
	}
	n := new(big.Int).SetUint64(uint64(mantissa))
	if exponent <= 3 {
		n.Rsh(n, uint(8*(3-exponent)))
	} else {
		n.Lsh(n, uint(8*(exponent-3)))
	}
	limit := new(big.Int).Lsh(big.NewInt(65535), 208)
	if n.Sign() <= 0 || n.Cmp(limit) > 0 {
		return target, fmt.Errorf("target outside mainnet range")
	}
	b := n.Bytes()
	for i := range b {
		target[i] = b[len(b)-1-i]
	}
	return target, nil
}
