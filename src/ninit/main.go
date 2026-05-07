package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/go-envparse"
)

const (
	servicesDir  = "/etc/ninit/services"
	logsDir      = "/var/log/ninit/services"
	configDir    = "/etc/ninit/config"
	superviseBin = "/sbin/ninit-supervise"
	readyFile    = "/var/run/ninit.ok"
)

type superviseResult struct {
	cmd    *exec.Cmd
	exitCh chan int // receives exit code when process dies
}

type serviceEntry struct {
	name   string
	pid    int
	result superviseResult
}

func getConfig(service, key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	f, err := os.Open(filepath.Join(configDir, service))
	if err != nil {
		return defaultValue
	}
	defer f.Close()

	config, err := envparse.Parse(f)
	for configKey, configValue := range config {
		if configKey == key {
			return configValue
		}
	}

	return defaultValue
}

func matchesPattern(service string, re *regexp.Regexp) bool {
	f, err := os.Open(filepath.Join(logsDir, service, "stderr"))
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			return true
		}
	}
	return false
}

func main() {
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ninit] failed to read %s: %v\n", servicesDir, err)
		os.Exit(1)
	}

	fmt.Println("[ninit] Starting services...")

	type startResult struct {
		entry *serviceEntry
		err   error
	}

	resultCh := make(chan startResult, len(entries))

	for _, entry := range entries {
		go func(service string) {
			cmd := exec.Command(superviseBin, service)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				resultCh <- startResult{err: fmt.Errorf("failed to start supervise for %s: %w", service, err)}
				return
			}

			exitCh := make(chan int, 1)
			diedCh := make(chan struct{}, 1)
			go func() {
				code := 0
				if err := cmd.Wait(); err != nil {
					if exitErr, ok := err.(*exec.ExitError); ok {
						code = exitErr.ExitCode()
					}
				}
				exitCh <- code
				diedCh <- struct{}{}
			}()

			result := superviseResult{cmd: cmd, exitCh: exitCh}
			pid := cmd.Process.Pid

			if getConfig(service, "NINIT_BACKGROUND", "true") != "true" {
				resultCh <- startResult{entry: &serviceEntry{service, pid, result}}
				return
			}

			retries, _ := strconv.Atoi(getConfig(service, "NINIT_BACKGROUND_RETRY_COUNT", "3"))
			intervalSec, _ := strconv.ParseFloat(getConfig(service, "NINIT_BACKGROUND_RETRY_INTERVAL", "0.1"), 64)
			timeout := time.Duration(retries) * time.Duration(intervalSec*float64(time.Second))
			re := regexp.MustCompile(getConfig(service, "NINIT_BACKGROUND_FAILURE_PATTERN", `^failed |level=fatal|panic:|^Fatal: `))

			crashed := false
			select {
			case <-diedCh:
				crashed = true
			case <-time.After(timeout):
				crashed = matchesPattern(service, re)
			}

			if crashed {
				resultCh <- startResult{entry: &serviceEntry{service, pid, result}}
			} else {
				resultCh <- startResult{}
			}
		}(entry.Name())
	}

	var waitList []serviceEntry
	for range entries {
		r := <-resultCh
		if r.err != nil {
			fmt.Fprintln(os.Stderr, "[ninit]", r.err)
			os.Exit(1)
		}
		if r.entry != nil {
			waitList = append(waitList, *r.entry)
		}
	}

	pids := make([]string, len(waitList))
	for i, e := range waitList {
		pids[i] = fmt.Sprintf("%s:%d", e.name, e.pid)
	}
	fmt.Printf("[ninit] Collected all PIDs: %s\n", strings.Join(pids, " "))

	worst := 0
	worstService := ""

	for _, e := range waitList {
		code := <-e.result.exitCh
		fmt.Printf("[ninit] Service '%s' (PID %d) exited with code %d\n", e.name, e.pid, code)
		if code > worst {
			worst = code
			worstService = e.name
		}
	}

	fmt.Printf("[ninit] Worst PID collected: %d\n", worst)

	if worst != 0 {
		if f, err := os.Open(filepath.Join(logsDir, worstService, "stderr")); err == nil {
			defer f.Close()
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}
		}
		os.Exit(worst)
	}

	if err := os.WriteFile(readyFile, nil, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[ninit] failed to write ready file: %v\n", err)
	}

	cmdArgs := os.Args[1:]
	fmt.Printf("[ninit] All good, spawning '%s'\n", strings.Join(cmdArgs, " "))
	binary, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ninit] binary not found: %v\n", err)
		os.Exit(1)
	}
	if err := syscall.Exec(binary, cmdArgs, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "[ninit] exec failed: %v\n", err)
		os.Exit(1)
	}
}
