// wasi-worker 只在受控进程中消费有界私有 IPC，不接纳业务命令或持有凭据。
package main

import (
	"os"

	"github.com/ruipengliu/lerna/adapters/wasi"
)

func main() {
	if err := wasi.RunWorker(os.Stdin, os.Stdout); err != nil {
		os.Exit(2)
	}
}
