package quicktar

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
)

const (
	EncNone   = 0
	EncAES128 = 1
	EncAES192 = 2
	EncAES256 = 3
)

var nonce = []byte{251, 79, 149, 47, 194, 100, 130, 101}

type Cipher struct {
	block cipher.Block
}

func NewCipher(enc int, pwd []byte) Cipher {
	if enc < EncNone || enc > EncAES256 {
		panic("invalid encryption method")
	}
	if enc == EncNone {
		return Cipher{}
	}
	sum := sha256.Sum256(pwd)
	keyLen := (enc + 1) * 8
	block, err := aes.NewCipher(sum[:keyLen])
	if err != nil {
		panic(err)
	}
	return Cipher{block}
}

var Store = NewCipher(EncNone, nil)

func (c *Cipher) xorKeyStream(dst, src []byte, off int64) {
	if c.block == nil {
		return
	}
	iv := make([]byte, 16)
	copy(iv, nonce)
	bn := off / 16
	binary.BigEndian.PutUint64(iv[8:], uint64(bn))
	ctr := cipher.NewCTR(c.block, iv)
	ctr.XORKeyStream(dst, src)
}

func BaseName(path string) string {
	lastSlash := -1
	for i, c := range path {
		if c == '/' {
			lastSlash = i
		}
	}
	if lastSlash == -1 {
		return path
	}
	return path[lastSlash+1:]
}

func Split(path string) []string {
	list := make([]string, 0)
	lastSlash := 0
	for i, c := range path {
		if c == '/' {
			list = append(list, path[lastSlash:i])
			lastSlash = i + 1
		}
	}
	return append(list, path[lastSlash:])
}
