//go:build amd64

package asm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"

	"git.tcp.direct/kayos/common/entropy"
)

const (
	goGen         = "go run asm.go -out checksum_amd64.s -stubs checksum_amd64.go"
	testEarlyFail = "early_fail"
	green         = "\033[32m"
	red           = "\033[31m"
	reset         = "\033[0m"
	testPassed    = green + "\n\ntest passed: " + reset + "%v\n"
	testFailed    = red + "\n\ntest failed: " + reset + "%v\n"
)

func TestRFC1071(t *testing.T) {
	type test struct {
		name   string
		input  []byte
		expect uint16
	}

	tests := []test{
		{
			name:   "zero length input",
			input:  []byte{},
			expect: 0,
		},
		{
			name:   "hello",
			input:  []byte("hello"),
			expect: 48173,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			actual := rfc1071(testCase.input)
			if actual != testCase.expect {
				t.Errorf(testFailed, fmt.Sprintf("Expected %v, but got %s%v%s", testCase.expect, red, actual, reset))
			} else {
				t.Logf(testPassed,
					string(testCase.input)+": "+strconv.Itoa(int(testCase.expect))+"=="+strconv.Itoa(int(actual)))
			}
		})
	}

}

func readPipe(ctx context.Context, pipePath string, u16bChan chan []byte, t *testing.T) {
	tryRead := func() (goOn bool) {
		f, err := os.OpenFile(pipePath, os.O_RDONLY, os.ModePerm)
		if err != nil {
			t.Errorf("%sfailed to open named pipe: %s%s", red, err, reset)
			return true
		}
		defer func() {
			if err = f.Close(); err != nil {
				panic(err)
			}
		}()
		b := make([]byte, 2)
		if _, err = f.Read(b); err != nil {
			t.Errorf("%sfailed to read from named pipe: %s%s", red, err, reset)
			return true
		}
		u16bChan <- b
		return false
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if !tryRead() {
				return
			}
		}
	}
}

func setup(t *testing.T) {
	for _, avo := range []string{"build", "reg", "operand"} {
		cmd := exec.Command("go", "get", "-v", "github.com/mmcloughlin/avo/"+avo)
		if cmdOut, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to get avo/%s: %s", avo, cmdOut)
		}
	}
}

func TestASMChecksum(t *testing.T) {
	type test struct {
		name   string
		input  []byte
		expect uint16
	}
	tests := []test{
		{
			name:   "zero length input",
			input:  []byte{},
			expect: 0,
		},
		{
			name:   "zero length input twice",
			input:  []byte{},
			expect: 0,
		},
		{
			name:   "hello",
			input:  []byte("hello"),
			expect: 48173,
		},
		{
			name:   "hello world",
			input:  []byte("hello world"),
			expect: rfc1071([]byte("hello world")),
		},
	}

	type entropic struct {
		value       []byte
		precomputed uint16
	}

	for i := 0; i < 64; i++ {
		r := entropy.AcquireRand()
		val := []byte(entropy.RandStrWithUpper(i * r.Intn(10)))
		for i := 0; i < len(val)-1; i++ {
			val[i] = val[i] ^ byte(r.Intn(255))
		}
		entropy.ReleaseRand(r)
		precomputed := rfc1071(val)
		tests = append(tests, test{
			name:   "entropy/" + strconv.Itoa(len(val)) + "b",
			input:  val,
			expect: precomputed,
		})
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			actual := checksum(testCase.input)
			if actual != testCase.expect {
				t.Errorf(testFailed, fmt.Sprintf("Expected %v, but got %s%v%s", testCase.expect, red, actual, reset))
			} else {
				t.Logf(testPassed,
					string(testCase.input)+": "+strconv.Itoa(int(testCase.expect))+"=="+strconv.Itoa(int(actual)))
			}
		})
	}
}

func BenchmarkChecksum(b *testing.B) {
	btests := [][]byte{
		[]byte("yeet"),
		[]byte("yeet world"),
		[]byte("world yeet"),
		[]byte("fuckhole jones"),
		bytes.Repeat([]byte("yeet"), 55),
		bytes.Repeat([]byte("yeet"), 156),
		bytes.Repeat([]byte("yeet"), 1024),
		bytes.Repeat([]byte("yeet"), 5001),
	}

	type candidate struct {
		name string
		f    func([]byte) uint16
	}

	var candidates = []candidate{
		{
			name: "golang",
			f:    rfc1071,
		},
		{
			name: "goasm",
			f:    checksum,
		},
	}

	for _, cn := range candidates {
		for _, data := range btests {
			if cn.name == "goasm" && runtime.GOARCH != "amd64" {
				continue
			}
			dlen := len(data)
			dlen64 := int64(dlen)
			b.Run(cn.name+"/"+strconv.Itoa(dlen), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					cn.f(data)
					b.SetBytes(dlen64)
				}
			})
		}
	}
}
