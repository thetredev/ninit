package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/hashicorp/go-envparse"
)

const (
	servicesDir = "/etc/ninit/services"
	configDir   = "/etc/ninit/config"
	logsDir     = "/var/log/ninit/services"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "[ninit-supervise] no service specified")
		os.Exit(1)
	}

	service := os.Args[1]

	// TODO: assert these all exist
	servicePath := filepath.Join(servicesDir, service)
	configPath := filepath.Join(configDir, service)

	execEnviron := os.Environ()

	configFile, err := os.Open(configPath)
	if err == nil {
		defer configFile.Close()
		config, err := envparse.Parse(configFile)

		if err != nil {
			fmt.Fprintf(os.Stderr, "[ninit-supervise] failed to parse config: %v\n", err)
			os.Exit(1)
		}
		for envKey, envVal := range config {
			execEnviron = append(execEnviron, fmt.Sprintf("%s=%s", envKey, envVal))
		}
	}

	logPath := filepath.Join(logsDir, service)

	if err := os.MkdirAll(logPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[ninit-supervise] failed to create log dir: %v\n", err)
		os.Exit(1)
	}

	stdout, err := os.OpenFile(filepath.Join(logPath, "stdout"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ninit-supervise] failed to open stdout log: %v\n", err)
		os.Exit(1)
	}

	stderr, err := os.OpenFile(filepath.Join(logPath, "stderr"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ninit-supervise] failed to open stderr log: %v\n", err)
		os.Exit(1)
	}

	if err := syscall.Dup2(int(stdout.Fd()), syscall.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "[ninit-supervise] failed to redirect stdout: %v\n", err)
		os.Exit(1)
	}
	if err := syscall.Dup2(int(stderr.Fd()), syscall.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "[ninit-supervise] failed to redirect stderr: %v\n", err)
		os.Exit(1)
	}

	stdout.Close()
	stderr.Close()

	if err := syscall.Exec(servicePath, []string{servicePath}, execEnviron); err != nil {
		fmt.Fprintf(os.Stderr, "[ninit-supervise] exec failed: %v\n", err)
		os.Exit(1)
	}
}
