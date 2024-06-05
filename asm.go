//go:build ignore

package main

import (
	"fmt"

	"github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/gotypes"
	"github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

/*
  +-----------------------------------------------------------------+
  |                          Main Memory                            |
  +-----------------------------------------------------------------+
                                  |
                                  |
                                  v
  +--------------------+        VMOVDQA           +------------------+
  | Vector Load-Store  +--------------------------> Scalar Registers |
  +--------------------+          |               +------------------+
                                  |                        ^
                                  |                        |
                                  v                        |
  +--------------------+       VMOVDQU	                VMOVDQA
  |  Vector Registers  +-----------------------------------+
  +--------------------+
          |          |          |          |         |         |
          |          |          |          |         |         |
          v          v          v          v         v         v
     +--------+ +---------+ +-------+ +--------+ +-------+ +--------+
     | VPADDW | | VPMULLW | | VDIVPS| | VPADDD | | VPAND | |  MOVQ  |
     +--------+ +---------+ +-------+ +--------+ +-------+ +--------+
*/

type label string

const (
	loop       label = "loop"
	remainder  label = "remainder"
	rLoop      label = "remainder_loop"
	fin        label = "done"
	early_fail label = "early_fail"
)

func gp64() reg.Register {
	return build.GP64()
}

func gp32() reg.Register {
	return build.GP32()
}

func gp8() reg.Register {
	return build.GP8()
}

func store(src reg.Register, dst gotypes.Component) {
	build.Store(src, dst)
}

func define(funcName string, argName string, argType string, ret string) {
	build.TEXT(funcName, 0, fmt.Sprintf("func(%s %s) %s", argName, argType, ret))
}

func zero(reg reg.Register) {
	switch reg.Size() * 8 {
	case 8:
		build.XORQ(reg, reg)
	case 16:
		build.XORW(reg, reg)
	case 32:
		build.XORL(reg, reg)
	case 64:
		build.XORQ(reg, reg)
	case 128, 256:
		// AVX2: vector register zeroing
		build.VXORPS(reg, reg, reg)
	}
}

func lbl(name any) {
	switch name.(type) {
	case label:
		build.Label(string(name.(label)))
	case string:
		build.Label(name.(string))
	default:
		panic("r u ok")
	}
}

func to(name label) operand.LabelRef {
	return operand.LabelRef(name)
}

func jae(a, b reg.Register, label operand.LabelRef) {
	build.CMPQ(a, b)
	build.JAE(label)
}

func testqjz(reg reg.Register, label operand.LabelRef) {
	build.TESTQ(reg, reg)
	build.JZ(label)
}

func jump(label operand.LabelRef) {
	build.JMP(label)
}

func constant(value uint64) operand.Constant {
	return operand.Imm(value)
}

func main() {
	define("checksum", "data", "[]byte", "uint16")
	input := build.Param("data")
	data := operand.Mem{Base: build.Load(input.Base(), gp64())}
	length := build.Load(input.Len(), gp64())

	build.TESTQ(length, length)
	build.JZ(to(fin))

	// ===================================================
	/*              REGISTER INITIALIZATION:            */
	// ===================================================

	// 64-bit register for sum
	sum := build.GP64()
	zero(sum)

	// 64-bit register for index
	index := gp64()
	zero(index)

	// 256-bit vector register for sum
	vectorSum := build.YMM()
	zero(vectorSum)

	// 256-bit vector register for data
	vectorData := build.YMM()
	zero(vectorData)

	// ---------------------------------------------------

	// ===================================================
	/*                      MAIN LOOP:                  */
	lbl("loop") // =======================================

	// if index >= length: goto remainder
	jae(index, length, to(remainder))

	/*
		(V)ector (MOV) (D)ouble (Q)uadword (U)naligned
		   move 128 bits to 256-bit vector register
	*/
	build.VMOVDQU(vectorData, data.Idx(index, 1))

	/*
		(V)ector (P)acked (ADD) 16-bit (W)ord integers
				   add the
		 - low 128-bits of 'vectorData'
				   and the
		 - high 128-bits of 'vectorSum'
		          into the
		 - lower half (128) of 'vectorSum'
	*/
	build.VPADDW(vectorSum, vectorSum, vectorData)

	// index += 32 bytes
	build.ADDQ(operand.U32(32), index)

	// goto loop label to continue accumulating data
	jump(to(loop))

	// ===================================================
	/*            REMAINDER && REMAINDER LOOP:          */
	lbl(remainder) // ====================================

	remaining := gp64()

	// calculate remaining bytes by
	// subtracting the index from the length
	build.MOVQ(remaining, length)
	build.SUBQ(remaining, index)

	lbl(rLoop) /* ----------- REMAINDER LOOP ----------- */

	// if remaining == 0: goto done
	testqjz(remaining, to(fin))

	// load odd byte into 8-bit register
	byteData := gp64()
	build.MOVB(byteData.(reg.GPVirtual).As8(), data.Idx(index, 1))
	// add byte to sum
	build.ADDQ(byteData, sum)
	// index++
	build.INCQ(index)
	// remaining--
	build.DECQ(remaining)
	// goto remainder loop
	jump(to(rLoop))

	// ===================================================
	/*                 FINAL WRAP-AROUND:               */
	lbl(fin) // ==========================================

	// -----
	// Reduce vector sum to a scalar sum (to 16 bits)
	// -----

	// 256 --> 128
	unpacked128 := build.XMM()
	// extract high 128-bits of vectorSum into unpacked128
	build.VEXTRACTI128(constant(1), vectorSum, unpacked128)
	// add unpacked to vectorSum, storing result in vectorSum
	build.VPADDW(vectorSum.AsY(), vectorSum.AsY(), unpacked128.AsY())

	build.VEXTRACTI128(constant(0), vectorSum, unpacked128)
	build.VPADDW(vectorSum.AsY(), vectorSum.AsY(), unpacked128.AsY())

	// 128 --> 64
	tmp128 := build.XMM()
	build.VMOVDQU(tmp128, vectorSum.AsX())

	tmp64 := gp64()
	// mov lower 32 bits of tmp128 to tmp64
	build.MOVD(tmp64, tmp128)
	build.ADDQ(sum, tmp64)
	// (P)acked (S)hift (R)ight (L)ogical (D)ouble (Q)uadword
	build.PSRLDQ(constant(8), tmp128.AsX())
	// mov lower 32 bits of tmp128 to tmp64
	build.MOVD(tmp128, tmp64)
	// add tmp64 to sum
	build.ADDQ(sum, tmp64)

	// 64 --> 32
	tmp32 := gp32()
	build.MOVL(tmp32, sum.As32())
	build.SHRL(constant(16), tmp32)
	build.ADDL(sum.As32(), tmp32)

	// 32 --> 16
	tmp16 := gp32()
	build.MOVL(tmp16, tmp32)
	build.SHRL(constant(16), tmp16)
	build.ADDL(sum.As32(), tmp16)
	build.ANDQ(operand.U32(0xFFFF), sum)

	store(sum.As16(), build.ReturnIndex(0))
	build.RET()

	// ===================================================
	/*                   EARLY FAIL:                    */
	lbl(early_fail) // ===================================
	retReg := build.GP16()
	build.XORW(retReg, retReg)
	build.MOVW(operand.U16(0), retReg)
	build.Store(retReg, build.ReturnIndex(0))
	build.RET()

	build.Generate()
}
