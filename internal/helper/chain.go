package helper

import (
	"fmt"
	"github.com/jiang2012/caffeine/pkg/logging"
	"go.uber.org/zap"
	"runtime/debug"
)

type Process func(interface{}) interface{}
type ProcessWrapper func(Process) Process

type Chain struct {
	wrappers []ProcessWrapper
}

func NewChain(p ...ProcessWrapper) Chain {
	return Chain{p}
}

// Then decorates the given process with all ProcessWrappers in the chain.
//
// This:
// NewChain(m1, m2, m3).Then(process)
// is equivalent to:
// m1(m2(m3(process)))
func (c Chain) Then(p Process) Process {
	for i := range c.wrappers {
		p = c.wrappers[len(c.wrappers)-i-1](p)
	}
	return p
}

func RecoverFromPanic() ProcessWrapper {
	return func(p Process) Process {
		return func(d interface{}) interface{} {
			defer func() {
				if err := recover(); err != nil {
					e, ok := err.(error)
					if !ok {
						e = fmt.Errorf("%v", err)
					}

					// alarm?
					logging.Error("Panic", zap.Error(e), zap.ByteString("stack", debug.Stack()))
				}
			}()

			return p(d)
		}
	}
}
