package fleeterror

import (
	"bytes"
	"fmt"
	"runtime"
)

type StackTrace struct {
	pc []uintptr
}

func NewStackTrace(offset int) StackTrace {
	pc := make([]uintptr, 100)
	n := runtime.Callers(offset+2, pc)
	pc = pc[:n]

	return StackTrace{pc: pc}
}

func (st StackTrace) String() string {
	if len(st.pc) == 0 {
		return ""
	}

	frames := runtime.CallersFrames(st.pc)

	var buffer bytes.Buffer

	for {
		frame, hasNext := frames.Next()

		_, _ = fmt.Fprintf(&buffer, "\tat %s: %s:%d\n", frame.Function, frame.File, frame.Line)

		if !hasNext {
			break
		}
	}

	return buffer.String()
}
