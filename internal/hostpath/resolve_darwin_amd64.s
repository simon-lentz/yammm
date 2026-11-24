#include "textflag.h"

TEXT libc_realpath_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_realpath(SB)
GLOBL	·libcRealpathTrampolineAddr(SB), RODATA, $8
DATA	·libcRealpathTrampolineAddr(SB)/8, $libc_realpath_trampoline<>(SB)
