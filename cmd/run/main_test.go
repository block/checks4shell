package run

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"testing"
)

// TestMain is the function setting up the environment for the test
// It is used here as a way to mock up command and output for the
// os/exec.Command. The args[1] is the name of the mocked command,
// and args[2:] are the arguments.
func TestMain(m *testing.M) {
	flag.Parse()

	pid := os.Getpid()
	if os.Getenv("GO_EXEC_TEST_PID") == "" {
		os.Setenv("GO_EXEC_TEST_PID", strconv.Itoa(pid))

		code := m.Run()
		os.Exit(code)
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}

	name := args[0]
	args = args[1:]

	helperCmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", name)
		os.Exit(1)
	}

	res := helperCmd(args...)
	if res != nil {
		os.Stderr.WriteString(res.error)
		os.Exit(res.code)
	}

	os.Exit(0)

}
