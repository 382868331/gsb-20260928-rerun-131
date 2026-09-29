package main

import (
	"flag"
	"fmt"
	"os"

	"tl/internal/tl/pipeline"
	"tl/internal/tl/vm"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("tlr", flag.ContinueOnError)
	budget := fs.Int64("budget", vm.DefaultBudget, "maximum executed IR instructions")
	sourceName := fs.String("source", "", "source file name used in runtime error positions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: tlr [-budget N] [-source source.tl] program.tlir")
	}
	path := fs.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	prog, err := pipeline.ReadIR(f)
	if err != nil {
		return err
	}
	name := *sourceName
	if name == "" {
		name = path
	}
	machine := vm.New(prog, name, os.Stdout, *budget)
	code, err := machine.Run()
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(int(code))
	}
	return nil
}
