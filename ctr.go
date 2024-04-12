package quicktar

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
)

const (
	EncNone   = 0
	EncAES128 = 1
	EncAES192 = 2
	EncAES256 = 3
)

var deprecatedNonce = []byte{251, 79, 149, 47, 194, 100, 130, 101}

type Cipher struct {
	block cipher.Block
	nonce []uint64
}

var Store = Cipher{}

func isEncNone(enc int) bool {
	if enc < EncNone || enc > EncAES256 {
		panic("invalid encryption method")
	}
	return enc == EncNone
}

func newCipher(enc int, pwd []byte, nonce []uint64) Cipher {
	sum := sha256.Sum256(pwd)
	keyLen := (enc + 1) * 8
	block, err := aes.NewCipher(sum[:keyLen])
	if err != nil {
		panic(err)
	}
	return Cipher{block, nonce}
}

// NewCipherNonce creates a Cipher object with nonce.
// If nonce is nil, it will be initialized with random value.
func NewCipherNonce(enc int, pwd []byte, nonce []byte) Cipher {
	if isEncNone(enc) {
		return Store
	}
	if nonce == nil {
		nonce = make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			panic(err)
		}
	} else if len(nonce) != 16 {
		panic("nonce should be of length 16 when presents")
	}
	return newCipher(enc, pwd, []uint64{
		binary.BigEndian.Uint64(nonce[:8]),
		binary.BigEndian.Uint64(nonce[8:]),
	})
}

// NewCipher creates a Cipher object.
func NewCipher(enc int, pwd []byte) Cipher {
	if isEncNone(enc) {
		return Store
	}
	return newCipher(enc, pwd, nil)
}

func (c *Cipher) xorKeyStream(dst, src []byte, off int64) {
	if c.block == nil {
		return
	}
	iv := make([]byte, 16)
	bn := uint64(off / 16)
	ivh := c.nonce[0]
	ivl := c.nonce[1] + bn
	if ivl < c.nonce[1] || ivl < bn { // overflow
		ivh++
	}
	binary.BigEndian.PutUint64(iv[:8], ivh)
	binary.BigEndian.PutUint64(iv[8:], ivl)
	ctr := cipher.NewCTR(c.block, iv)
	ctr.XORKeyStream(dst, src)
}
