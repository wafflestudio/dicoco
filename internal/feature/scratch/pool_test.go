package scratch

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAddress = "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"

func TestWalletAddress(t *testing.T) {
	script, err := addressScript(testAddress)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(script); got != "0014751e76e8199196d454941c45d1b3a323f1433bd6" {
		t.Fatal(got)
	}
	for _, bad := range []string{testAddress[:len(testAddress)-1] + "q", "tb" + testAddress[2:], "bC" + testAddress[2:], "", strings.Repeat("q", 100)} {
		if _, err := addressScript(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func coinbaseFixture(script []byte, ours uint64) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(2))
	b.WriteByte(1)
	b.Write(make([]byte, 32))
	binary.Write(&b, binary.LittleEndian, uint32(0xffffffff))
	b.WriteByte(12)
	b.Write([]byte{3, 0x40, 0xd1, 0x0c})
	b.Write(make([]byte, 8)) // Height 840000 and extranonces.
	binary.Write(&b, binary.LittleEndian, uint32(0xffffffff))
	b.WriteByte(2)
	binary.Write(&b, binary.LittleEndian, ours)
	b.WriteByte(byte(len(script)))
	b.Write(script)
	binary.Write(&b, binary.LittleEndian, uint64(312500000)-ours)
	b.WriteByte(1)
	b.WriteByte(0x51)
	binary.Write(&b, binary.LittleEndian, uint32(0))
	return b.Bytes()
}

func TestCoinbasePayout(t *testing.T) {
	script, _ := addressScript(testAddress)
	valid := coinbaseFixture(script, 306250000)
	if err := validateCoinbase(valid, script); err != nil {
		t.Fatal(err)
	}
	wrong := append([]byte(nil), script...)
	wrong[2] ^= 1
	if err := validateCoinbase(valid, wrong); err == nil {
		t.Fatal("accepted other wallet")
	}
	if err := validateCoinbase(coinbaseFixture(script, 1), script); err == nil {
		t.Fatal("accepted dust payout")
	}
	for i := 0; i < len(valid); i++ {
		if err := validateCoinbase(valid[:i], script); err == nil {
			t.Fatalf("accepted truncation %d", i)
		}
	}
	if err := validateCoinbase(append(valid, 0), script); err == nil {
		t.Fatal("accepted trailing bytes")
	}
}

func TestGenesisHashAndTarget(t *testing.T) {
	header, _ := hex.DecodeString("01000000" + strings.Repeat("00", 32) + "3ba3edfd7a7b12b27ac72c3e67768f617fc81bc3888a51323a9fb8aa4b1e5e4a29ab5f49ffff001d1dac2b7c")
	hash := doubleHash(header)
	if HashString(hash) != "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f" {
		t.Fatal(HashString(hash))
	}
	target, err := compactTarget(0x1d00ffff)
	if err != nil || lessBitcoinHash(target, hash) {
		t.Fatal("genesis does not pass target", err)
	}
	for _, bad := range []uint32{0, 0x1d80ffff, 0x2300ffff, 0x207fffff, 0x01000001} {
		if _, err := compactTarget(bad); err == nil {
			t.Fatalf("accepted target %x", bad)
		}
	}
}

func jobFixture(script []byte) []any {
	coin := coinbaseFixture(script, 306250000)
	return []any{"job", strings.Repeat("01234567", 8), hex.EncodeToString(coin[:46]), hex.EncodeToString(coin[54:]), []string{}, "20000000", "170fffff", "65000000", true}
}

func TestJobHeader(t *testing.T) {
	script, _ := addressScript(testAddress)
	b, _ := json.Marshal(jobFixture(script))
	var fields []json.RawMessage
	json.Unmarshal(b, &fields)
	j, err := parseJob(fields)
	if err != nil {
		t.Fatal(err)
	}
	j.branches = [][]byte{make([]byte, 32)}
	header, _, err := j.header(make([]byte, 4), make([]byte, 4), script)
	if err != nil {
		t.Fatal(err)
	}
	root := doubleHash(coinbaseFixture(script, 306250000))
	pair := append(root[:], make([]byte, 32)...)
	root = doubleHash(pair)
	if !bytes.Equal(header[36:68], root[:]) || hex.EncodeToString(header[4:8]) != "67452301" || binary.LittleEndian.Uint32(header[68:]) != 0x65000000 {
		t.Fatal("incorrect header byte order or merkle root")
	}
}

func TestSessionHandshakeAndRun(t *testing.T) {
	script, _ := addressScript(testAddress)
	client, server := net.Pipe()
	defer server.Close()
	serverErr := make(chan error, 1)
	go func() {
		dec, enc := json.NewDecoder(server), json.NewEncoder(server)
		var request struct {
			ID     int
			Method string
			Params []string
		}
		if err := dec.Decode(&request); err != nil {
			serverErr <- err
			return
		}
		if request.Method != "mining.subscribe" {
			serverErr <- fmt.Errorf("unexpected subscription")
			return
		}
		// Notifications may arrive before either RPC response.
		enc.Encode(map[string]any{"method": "mining.notify", "params": jobFixture(script)})
		enc.Encode(map[string]any{"id": 1, "result": []any{[]any{}, "00000000", 4}, "error": nil})
		if err := dec.Decode(&request); err != nil {
			serverErr <- err
			return
		}
		if request.Method != "mining.authorize" || request.Params[0] != testAddress+".dicoco" {
			serverErr <- fmt.Errorf("unexpected authorization")
			return
		}
		enc.Encode(map[string]any{"id": 2, "result": true, "error": nil})
		serverErr <- nil
	}()
	p := newPoolClient()
	p.load = func() (string, []byte, error) { return testAddress, script, nil }
	p.dial = func(context.Context) (net.Conn, error) { return client, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.connect(ctx) }()
	result, err := p.run(10000)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 10000 || result.Score < 1 || result.Score > 99 {
		t.Fatalf("unexpected result %+v", result)
	}
	// Repeated and concurrent commands reuse this one connection.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.run(10000); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	work, err := p.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldCtx := work.ctx
	params := jobFixture(script)
	params[0] = "new-job"
	if err := json.NewEncoder(server).Encode(map[string]any{"method": "mining.notify", "params": params}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-oldCtx.Done():
	case <-ctx.Done():
		t.Fatal("old job not invalidated")
	}
	if _, err := p.run(10000); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown stuck")
	}
}

func TestSessionTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	p := newPoolClient()
	p.load = func() (string, []byte, error) { return testAddress, nil, nil }
	p.dial = func(context.Context) (net.Conn, error) { return client, nil }
	if err := p.connect(ctx); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestReconnectAndShutdown(t *testing.T) {
	p := newPoolClient()
	p.retry = time.Millisecond
	script, _ := addressScript(testAddress)
	p.load = func() (string, []byte, error) { return testAddress, script, nil }
	connections := make(chan net.Conn, 4)
	p.dial = func(context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		connections <- server
		return client, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); p.serve(ctx, log.New(io.Discard, "", 0)) }()
	for i := 0; i < 2; i++ {
		var server net.Conn
		select {
		case server = <-connections:
		case <-ctx.Done():
			t.Fatal("did not reconnect")
		}
		server.Close()
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown stuck")
	}
}

func TestUniqueExtranonces(t *testing.T) {
	c := &liveConnection{nonce: make([]byte, 4)}
	results := make(chan string, 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := c.nextNonce()
			if err != nil {
				t.Error(err)
				return
			}
			results <- hex.EncodeToString(n)
		}()
	}
	wg.Wait()
	close(results)
	seen := make(map[string]bool)
	for n := range results {
		if seen[n] {
			t.Fatal("duplicate extranonce")
		}
		seen[n] = true
	}
	c.nonce = []byte{255}
	c.exhausted = false
	if _, err := c.nextNonce(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.nextNonce(); err == nil {
		t.Fatal("nonce wrapped")
	}
}

func TestSubmitCandidate(t *testing.T) {
	for _, accept := range []bool{true, false} {
		t.Run(fmt.Sprint(accept), func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			s := &poolSession{conn: client, events: make(chan poolEvent, 8)}
			go s.read(ctx)
			requests := make(chan []string, 1)
			go func() {
				var req struct{ Params []string }
				json.NewDecoder(server).Decode(&req)
				requests <- req.Params
				json.NewEncoder(server).Encode(map[string]any{"id": 3, "result": accept, "error": nil})
			}()
			status, err := s.submit(ctx, "wallet.dicoco", &poolJob{id: "job", ntime: "65000000"}, []byte{1, 2, 3, 4}, 0x12345678)
			if (err == nil) != accept || (status == "accepted") != accept {
				t.Fatal(status, err)
			}
			params := <-requests
			if strings.Join(params, ",") != "wallet.dicoco,job,01020304,65000000,12345678" {
				t.Fatal(params)
			}
		})
	}
}
