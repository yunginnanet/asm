//go:build !amd64

package asm

func checksum(data []byte) uint16 {
	return rfc1071(data)
}
