// baton: Claude Code 를 계정별로 띄우는 래퍼 (orca-baton).
package main

import (
	"os"

	"github.com/juunghyun/orca-baton/internal/baton"
)

func main() { os.Exit(baton.Main(os.Args)) }
