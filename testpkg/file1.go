package testpkg

import (
	"sync/atomic"
)

var __callId int64
var ProxyInvocationHandler func(funcName string, callId int64)

func __invokeHandler(funcName string, callId int64) (string, int64) {
	if ProxyInvocationHandler == nil {
		return funcName, callId
	}
	if callId == -1 {
		callId = atomic.AddInt64(&callId, 1)
	}
	ProxyInvocationHandler(funcName, callId)
	return funcName, callId
}
