// wasi-worker 只在受控进程中消费有界私有 IPC，不接纳业务命令或持有凭据。
package main

import (
	"os"

	"github.com/ruipengliu/lerna/adapters/wasi"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--probe" {
		if err := wasi.ProbeWorker(os.Stdout); err != nil {
			os.Exit(2)
		}
		return
	}
	if len(os.Args) != 1 {
		os.Exit(2)
	}
	if err := wasi.RunWorker(os.Stdin, os.Stdout); err != nil {
		os.Exit(2)
	}
}
