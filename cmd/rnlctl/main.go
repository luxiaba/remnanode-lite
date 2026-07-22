package main

import (
	"context"
	"os"

	"github.com/luxiaba/remnanode-lite/internal/rnlctl"
)

func main() {
	application := rnlctl.New(rnlctl.Options{})
	if code := application.Run(context.Background(), os.Args[1:]); code != 0 {
		os.Exit(code)
	}
}
