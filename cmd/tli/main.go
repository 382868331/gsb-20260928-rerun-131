package main

import (
	"flag"
	"fmt"
	"os"

	"tl/internal/tl/interp"
	"tl/internal/tl/pipeline"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("tli", flag.ContinueOnError)
	budget := fs.Int64("budget", interp.DefaultBudget, "maximum evaluated AST steps")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: tli [-budget N] source.tl")
	}
	path := fs.Arg(0)
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	parsed, err := pipeline.Parse(path, src)
	if err != nil {
		return err
	}
	if _, err := pipeline.Check(path, parsed); err != nil {
		return err
	}
	machine := interp.New(path, parsed, os.Stdout, *budget)
	code, err := machine.Run()
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(int(code))
	}
	return nil
}
