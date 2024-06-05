// //go:build ignore

package main

import (
	"fmt"

	"github.com/mmcloughlin/avo/attr"
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
	loop      label = "loop"
	remainder label = "remainder"
	rLoop     label = "remainder_loop"
	fin       label = "done"
)

func gp64() reg.Register {
	return build.GP64()
}

func gp8() reg.Register {
	return build.GP8()
}

func arg(name string) gotypes.Component {
	if name == "" {
		panic("r u ok")
	}
	return build.Param(name)
}

func load(src gotypes.Component, dst reg.Register) reg.Register {
	return build.Load(src, dst)
}

func store(src reg.Register, dst gotypes.Component) {
	build.Store(src, dst)
}

func define(funcName string, argName string, argType string, ret string) {
	build.TEXT(funcName, attr.NOSPLIT, fmt.Sprintf("func(%s %s) %s", argName, argType, ret))
}

func ptr(base reg.Register) operand.Mem {
	return operand.Mem{Base: base}
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
	case 256:
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
	define("ChecksumAVX2", "data", "[]byte", "uint16")
	data := ptr(load(arg("data"), gp64()))
	length := load(arg("data").Len(), gp64())

	// ===================================================
	/*			   REGISTER INITIALIZATION:			    */
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

	// ---------------------------------------------------

	// ===================================================
	/*  	      		   MAIN LOOP:	  			    */
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
	build.ADDQ(index, operand.U64(32))

	// goto loop label to continue accumulating data
	jump(to(loop))

	// ===================================================
	/* 			  REMAINDER && REMAINDER LOOP:          */
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
	byteData := gp8()
	build.MOVB(byteData, data.Idx(index, 1))
	// add byte to sum
	build.ADDQ(sum, byteData)
	// index++
	build.INCQ(index)
	// remaining--
	build.DECQ(remaining)
	// goto remainder loop
	jump(to(rLoop))

	// ===================================================
	/*	 			  FINAL WRAP-AROUND:	            */
	lbl(fin) // ==========================================

	// -----
	// Reduce vector sum to a scalar sum (to 16 bits)
	// -----

	// 256 --> 128
	// 128 bit vector register
	unpacked128 := build.XMM()
	// extract high 128-bits of vectorSum into unpacked
	build.VEXTRACTI128(constant(1), vectorSum, unpacked128)
	// add unpacked to vectorSum, storing result in vectorSum
	build.VPADDW(vectorSum, vectorSum, unpacked128)

	// 128 --> 64
	// 64 bit register
	unpacked64 := build.GP64()
	// mov packed doubleword (vectorSum) integers to quadword (unpacked64)
	build.VPMOVZXDQ(unpacked64, vectorSum)
	// add unpacked64 to sum (both 64-bit registers)
	build.ADDQ(sum, unpacked64)

	// 64 --> 32
	// (P)acked (S)hift (Right) (L)ogical (D)ouble (Q)uadword
	// shift vectorSum (256 bits) right by 8 bytes (64 bits)
	build.PSRLDQ(constant(8), vectorSum)
	build.VPMOVZXDQ(unpacked64, vectorSum)
	build.ADDQ(sum, unpacked64)

	// 32 --> 16
	shifted := gp64()
	build.MOVQ(shifted, sum)
	build.SHRQ(shifted, constant(16))
	build.ADDQ(sum, shifted)
	build.ANDQ(sum, constant(0xFFFF))

	store(sum, build.ReturnIndex(0))

	build.Generate()
}
