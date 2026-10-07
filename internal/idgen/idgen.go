// Package idgen produces short, unique, URL-safe message identifiers without
// external dependencies.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"
)

var counter uint64

// New returns a time-ordered, collision-resistant identifier.
func New() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	n := atomic.AddUint64(&counter, 1)
	ts := time.Now().UnixNano()
	buf := make([]byte, 0, 32)
	buf = appendHex(buf, uint64(ts))
	buf = appendHex(buf, n)
	buf = append(buf, hex.EncodeToString(b[:])...)
	return string(buf)
}

func appendHex(dst []byte, v uint64) []byte {
	var tmp [16]byte
	hex.Encode(tmp[:], []byte{
		byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
		byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
	})
	return append(dst, tmp[:]...)
}
