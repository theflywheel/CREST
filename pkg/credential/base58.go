package credential

import (
	"fmt"
	"math/big"
	"strings"
)

// Base58btc, because that is what multibase 'z' means and what the Data
// Integrity proof format specifies for a proofValue. Twenty lines is cheaper
// than a dependency for something this small and this stable.

const b58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base58Encode(in []byte) string {
	// Leading zero bytes are not representable positionally, so they are
	// emitted as leading '1's — the standard encoding, and the reason a
	// signature with a zero first byte round-trips.
	zeros := 0
	for zeros < len(in) && in[zeros] == 0 {
		zeros++
	}
	n := new(big.Int).SetBytes(in)
	radix := big.NewInt(58)
	mod := new(big.Int)
	var out []byte
	for n.Sign() > 0 {
		n.DivMod(n, radix, mod)
		out = append(out, b58[mod.Int64()])
	}
	for i := 0; i < zeros; i++ {
		out = append(out, b58[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func base58Decode(s string) ([]byte, error) {
	n := new(big.Int)
	radix := big.NewInt(58)
	for _, r := range s {
		i := strings.IndexRune(b58, r)
		if i < 0 {
			return nil, fmt.Errorf("base58: %q is not a base58btc character", r)
		}
		n.Mul(n, radix)
		n.Add(n, big.NewInt(int64(i)))
	}
	out := n.Bytes()
	zeros := 0
	for zeros < len(s) && s[zeros] == b58[0] {
		zeros++
	}
	return append(make([]byte, zeros), out...), nil
}
