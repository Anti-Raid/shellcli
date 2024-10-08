// From swaweedfs util/grace

package shell

import (
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"sync"
	"syscall"
)

var signalChan chan os.Signal
var interruptHooks = make([]func(), 0)
var interruptHookLock sync.RWMutex
var reloadHooks = make([]func(), 0)
var reloadHookLock sync.RWMutex

func GetFunctionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}

func init() {
	signalChan = make(chan os.Signal, 1)
	signal.Notify(signalChan,
		os.Interrupt,
		syscall.SIGALRM,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		// syscall.SIGQUIT,
	)
	go func() {
		for s := range signalChan {
			if s.String() == syscall.SIGHUP.String() {
				reloadHookLock.RLock()
				for _, hook := range reloadHooks {
					hook()
				}
				reloadHookLock.RUnlock()
			} else {
				interruptHookLock.RLock()
				for _, hook := range interruptHooks {
					fmt.Printf("exec interrupt hook func name:%s", GetFunctionName(hook))
					hook()
				}
				interruptHookLock.RUnlock()
				os.Exit(0)
			}
		}
	}()
}

func OnReload(fn func()) {
	// prevent reentry
	reloadHookLock.Lock()
	defer reloadHookLock.Unlock()
	reloadHooks = append(reloadHooks, fn)
}

func OnInterrupt(fn func()) {
	// prevent reentry
	interruptHookLock.Lock()
	defer interruptHookLock.Unlock()

	// deal with control+c,etc
	// controlling terminal close, daemon not exit
	interruptHooks = append(interruptHooks, fn)
}
