package scratch

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

const walletFile = "/var/run/secrets/discord-bot/wallet"

func loadWallet() (string, []byte, error) {
	f, err := os.Open(walletFile)
	if err != nil {
		return "", nil, fmt.Errorf("read wallet: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 257))
	if err != nil || len(b) > 256 {
		return "", nil, fmt.Errorf("wallet file unreadable or too large")
	}
	address := strings.TrimSpace(string(b))
	script, err := addressScript(address)
	return strings.ToLower(address), script, err
}

// Only mainnet native SegWit v0 (bc1q) addresses are accepted. Validate
// BIP173's checksum and padding before deriving the exact output script.
func addressScript(address string) ([]byte, error) {
	if address != strings.ToLower(address) && address != strings.ToUpper(address) {
		return nil, fmt.Errorf("mixed-case wallet address")
	}
	address = strings.ToLower(address)
	if len(address) > 90 || len(address) < 14 || !strings.HasPrefix(address, "bc1q") {
		return nil, fmt.Errorf("wallet must be a mainnet bc1q address")
	}
	const alphabet = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
	values := []byte{3, 3, 0, 2, 3} // HRP expansion for "bc".
	for _, c := range address[3:] {
		i := strings.IndexRune(alphabet, c)
		if i < 0 {
			return nil, fmt.Errorf("invalid wallet character")
		}
		values = append(values, byte(i))
	}
	chk := uint32(1)
	gen := [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i, g := range gen {
			if top>>i&1 != 0 {
				chk ^= g
			}
		}
	}
	if chk != 1 {
		return nil, fmt.Errorf("invalid wallet checksum")
	}
	var program []byte
	var acc uint32
	bits := uint(0)
	for _, v := range values[6 : len(values)-6] {
		acc = (acc<<5 | uint32(v)) & 0xffff
		bits += 5
		if bits >= 8 {
			bits -= 8
			program = append(program, byte(acc>>bits))
		}
	}
	if bits >= 5 || (acc<<(8-bits))&255 != 0 || (len(program) != 20 && len(program) != 32) {
		return nil, fmt.Errorf("invalid wallet witness program")
	}
	return append([]byte{0, byte(len(program))}, program...), nil
}

func compactSize(r *bytes.Reader) (uint64, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if b < 253 {
		return uint64(b), nil
	}
	var v uint64
	switch b {
	case 253:
		var n uint16
		err = binary.Read(r, binary.LittleEndian, &n)
		v = uint64(n)
		if v < 253 {
			return 0, fmt.Errorf("noncanonical compact size")
		}
	case 254:
		var n uint32
		err = binary.Read(r, binary.LittleEndian, &n)
		v = uint64(n)
		if v <= 65535 {
			return 0, fmt.Errorf("noncanonical compact size")
		}
	case 255:
		err = binary.Read(r, binary.LittleEndian, &v)
		if v <= 0xffffffff {
			return 0, fmt.Errorf("noncanonical compact size")
		}
	}
	return v, err
}

func readScript(r *bytes.Reader, max uint64) ([]byte, error) {
	n, err := compactSize(r)
	if err != nil || n > max || n > uint64(r.Len()) {
		return nil, fmt.Errorf("invalid script length")
	}
	b := make([]byte, int(n))
	_, err = io.ReadFull(r, b)
	return b, err
}

// Stratum supplies the stripped (non-witness) coinbase for txid hashing.
// Enforce our script and at least 98% of both subsidy and supplied outputs.
// This cannot independently prove the height or the fees of hidden transactions.
func validateCoinbase(tx, script []byte) error {
	if len(tx) > 16000 {
		return fmt.Errorf("coinbase too large")
	}
	r := bytes.NewReader(tx)
	var version uint32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return err
	}
	n, err := compactSize(r)
	if err != nil || n != 1 {
		return fmt.Errorf("coinbase must have one stripped input")
	}
	var prev [36]byte
	if _, err := io.ReadFull(r, prev[:]); err != nil {
		return err
	}
	if !bytes.Equal(prev[:32], make([]byte, 32)) || binary.LittleEndian.Uint32(prev[32:]) != 0xffffffff {
		return fmt.Errorf("not a coinbase input")
	}
	sig, err := readScript(r, 100)
	if err != nil || len(sig) < 2 || sig[0] < 1 || sig[0] > 5 || int(sig[0])+1 > len(sig) {
		return fmt.Errorf("invalid coinbase height")
	}
	heightBytes := sig[1 : 1+int(sig[0])]
	if heightBytes[len(heightBytes)-1]&0x80 != 0 {
		return fmt.Errorf("negative height")
	}
	var height uint64
	for i, b := range heightBytes {
		height |= uint64(b) << (8 * i)
	}
	if height == 0 {
		return fmt.Errorf("zero height")
	}
	var sequence uint32
	if err := binary.Read(r, binary.LittleEndian, &sequence); err != nil {
		return err
	}
	n, err = compactSize(r)
	if err != nil || n == 0 || n > 128 {
		return fmt.Errorf("invalid output count")
	}
	const maxMoney = uint64(21_000_000 * 100_000_000)
	var total, ours uint64
	for i := uint64(0); i < n; i++ {
		var value uint64
		if err := binary.Read(r, binary.LittleEndian, &value); err != nil {
			return err
		}
		if value > maxMoney || total > maxMoney-value {
			return fmt.Errorf("invalid output value")
		}
		out, err := readScript(r, 10000)
		if err != nil {
			return err
		}
		total += value
		if bytes.Equal(out, script) {
			ours += value
		}
	}
	var locktime uint32
	if err := binary.Read(r, binary.LittleEndian, &locktime); err != nil {
		return err
	}
	if r.Len() != 0 {
		return fmt.Errorf("trailing coinbase bytes")
	}
	subsidy := uint64(5_000_000_000) >> (height / 210000)
	if ours == 0 || total < subsidy || ours*100+100 < total*98 || ours*100+100 < subsidy*98 {
		return fmt.Errorf("coinbase does not pay wallet at least 98%%")
	}
	return nil
}
