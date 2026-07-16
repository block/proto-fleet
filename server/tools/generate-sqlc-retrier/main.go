package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	querierPath := flag.String("querier", "", "path to sqlc's generated querier.go")
	outPath := flag.String("out", "", "generated output path")
	flag.Parse()

	if *querierPath == "" || *outPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	generated, err := generate(*querierPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate retrying querier: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, generated, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write generated querier: %v\n", err)
		os.Exit(1)
	}
}
