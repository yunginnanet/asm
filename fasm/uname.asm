format ELF64 executable 3
entry yeet

; just getting my feet wet
; defining a struct
; doing some looping
; printing uname

; ---------- structs ---------

segment readable writeable

utsname:
	sysname    rb 65
	nodename   rb 65
	release    rb 65
	version    rb 65
	machine    rb 65
	domainname rb 65

linebreak db 0x0a
space db 0x20

; ----------------------------

segment readable executable

; notes ---------------
; - - - - - - - - - - -
; rax: syscall nr
; arg0: rdi
; arg1: rsi
; arg2: rdx
; arg3: r10
; arg4: r8
; arg5: r9
; ---------------------

yeet:
	; uname syscall -------
	; - - - - - - - - - - -
	; uname syscall is 63
	mov rax, 63
	; load struct addr->rdi
	lea rdi, [utsname]
	; execute the syscall
	;int 0x80
	;+*+*+*
	syscall
	;*+*+*+
	;----------------------

	; clear index register
	xor r12, r12
ohai:
	; -welcome to looptown-

	; clear arg1
	xor rsi, rsi

	; increment index
	add r12, 65

        ; ---------------------
	; write syscall -------
	; - - - - - - - - - - -
	mov rax, 1
	; stdout fd to arg0
	mov rdi, 1
	; r12 as index/offset
	lea rsi, [utsname+r12]
	; expec. len to arg2
	mov rdx, 65

	;+*+*+*
	syscall
	;*+*+*+

	; -------- spacebar
	mov rax, 1
	mov rdi, 1
	lea rsi, [space]
	mov rdx, 1

	;+*+*+*
	syscall
	;*+*+*+

	; - - - - - - - - - -^
	; if i < 305 goto ohai
	cmp r12, 305
	jle ohai
	; ~ ~ ~ ~ ~ ~ ~ ~ ~ ~^


	; -------- enter
	mov rax, 1
	mov rdi, 1
	lea rsi, [linebreak]
	mov rdx, 1

	;+*+*+*
	syscall
	;*+*+*+

	; ---------------------
	; exit syscall --------
	; - - - - - - - - - - -
	mov rax, 60
	; exit code to arg0
	mov rdi, 0
	syscall
	;----------------------
