package main

import (
	"flag"
	"fmt"
	"os"

	"tl/internal/tl/pipeline"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("tlc", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "parse and type-check only")
	out := fs.String("o", "a.tlir", "output IR file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: tlc [-check] [-o file] source.tl")
	}
	source := fs.Arg(0)
	src, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	result, err := pipeline.Compile(source, src)
	if err != nil {
		return err
	}
	if *checkOnly {
		fmt.Printf("ok: %s (%d functions)\n", source, len(result.Checked.Order))
		return nil
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := pipeline.WriteIR(f, result.IR); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	return nil
}
