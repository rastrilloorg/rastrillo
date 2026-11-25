// Command goplsmcp is the carlos-gopls arm's MCP server: `gopls mcp`
// behind internal/mcpproxy, which starts gopls again in the app's module
// once its go.mod appears. bench run builds it and names it in the
// arm's MCP config.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/mcpproxy"
)

func main() {
	gopls := flag.String("gopls", "", "the gopls binary")
	dir := flag.String("dir", "", "the app's directory; the working directory if empty")
	logFile := flag.String("log", "", "a file to log starts and restarts to")
	flag.Parse()
	if err := run(*gopls, *dir, *logFile); err != nil {
		fmt.Fprintln(os.Stderr, "goplsmcp:", err)
		os.Exit(1)
	}
}

func run(gopls, dir, logFile string) error {
	if gopls == "" {
		return fmt.Errorf("-gopls is required")
	}
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return err
		}
	}
	var log io.Writer
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer file.Close()
		log = file
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	proxy := &mcpproxy.Proxy{
		Start: func(dir string) (mcpproxy.Child, error) { return startGopls(gopls, dir) },
		Dir:   dir, Poll: time.Second, Log: log,
	}
	return proxy.Run(ctx, os.Stdin, os.Stdout)
}

// startGopls runs `gopls mcp` in dir. Its stdout is a pipe this process
// owns, not exec's StdoutPipe, so waiting for it to exit never closes
// the pipe under the proxy: what it wrote before exiting is still read,
// up to EOF. Stopping it closes its input, which ends it, and kills it
// if it has not gone within five seconds.
func startGopls(gopls, dir string) (mcpproxy.Child, error) {
	command := exec.Command(gopls, "mcp")
	command.Dir = dir
	command.Stderr = os.Stderr
	in, err := command.StdinPipe()
	if err != nil {
		return mcpproxy.Child{}, err
	}
	out, childOut, err := os.Pipe()
	if err != nil {
		return mcpproxy.Child{}, err
	}
	command.Stdout = childOut
	if err := command.Start(); err != nil {
		out.Close()
		childOut.Close()
		return mcpproxy.Child{}, err
	}
	childOut.Close() // the child has its own copy; EOF comes when it exits
	exited := make(chan struct{})
	go func() {
		command.Wait()
		close(exited)
	}()
	return mcpproxy.Child{In: in, Out: out, Stop: func() {
		in.Close()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			command.Process.Kill()
			<-exited
		}
	}}, nil
}
