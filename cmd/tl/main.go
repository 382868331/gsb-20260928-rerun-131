package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"teachinglang/internal/tl"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	command := os.Args[1]
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	budget := fs.Int64("budget", 1_000_000, "maximum executed statements, instructions and calls")
	output := fs.String("o", "", "output IR file for compile")
	_ = fs.Parse(os.Args[2:])
	args := fs.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	var err error
	switch command {
	case "compile":
		if len(args) != 1 || *output == "" {
			fmt.Fprintln(os.Stderr, "usage: tl compile -o program.tli program.tl")
			os.Exit(2)
		}
		err = compile(args[0], *output)
	case "run":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: tl run [-budget N] program.tli")
			os.Exit(2)
		}
		err = runIR(args[0], *budget)
	case "interpret":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: tl interpret [-budget N] program.tl")
			os.Exit(2)
		}
		err = interpret(args[0], *budget)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() { fmt.Fprintln(os.Stderr, "usage: tl <compile|run|interpret> [flags] file") }

func compile(sourcePath, outputPath string) error {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	ast, err := tl.Parse(string(source))
	if err != nil {
		return err
	}
	checked, err := tl.Check(ast)
	if err != nil {
		return err
	}
	ir, err := tl.Generate(checked)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(ir, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(data, '\n'), 0o644)
}

func runIR(path string, budget int64) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	ir, err := tl.DecodeIR(data)
	if err != nil {
		return err
	}
	vm, err := tl.NewVM(ir, budget, os.Stdout)
	if err != nil {
		return err
	}
	exitCode, err := vm.Run()
	if err != nil {
		return err
	}
	if exitCode != 0 {
		os.Exit(int(exitCode))
	}
	return nil
}

func interpret(path string, budget int64) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	ast, err := tl.Parse(string(source))
	if err != nil {
		return err
	}
	checked, err := tl.Check(ast)
	if err != nil {
		return err
	}
	interpreter, err := tl.NewInterpreter(checked, budget, os.Stdout)
	if err != nil {
		return err
	}
	exitCode, err := interpreter.Run()
	if err != nil {
		return err
	}
	if exitCode != 0 {
		os.Exit(int(exitCode))
	}
	return nil
}
