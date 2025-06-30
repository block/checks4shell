package run

import (
	"encoding/json"
	"fmt"
	"github.com/google/go-github/v64/github"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var exeOnce struct {
	path string
	err  error
	sync.Once
}

// exePath returns the path to the test binaries
func exePath(t testing.TB) string {
	t.Helper()
	exeOnce.Do(func() {
		exeOnce.path, exeOnce.err = os.Executable()
	})

	if exeOnce.err != nil {
		t.Fatal(exeOnce.err)
	}

	return exeOnce.path
}

type errExitCode struct {
	code  int
	error string
}

var commands = map[string]func(args ...string) *errExitCode{
	"echo":            cmdEcho,
	"prints":          cmdPrintWithSleep,
	"errorm":          cmdErrorMaker,
	"summary-f":       cmdSummaryF,
	"gen-image":       cmdGenerateImages,
	"gen-annotations": cmdGenerateAnnotations,
	"cat-big-uni":     cmdCatBigUnicode,
	"repeat-summary":  cmdRepeatSummary,
	"capture-signal":  cmdCaptureSignal,
}

// command returns the command executable that redirects back to the commands defined
// in this file
func command(t *testing.T, name string, args ...string) []string {
	return append([]string{
		exePath(t),
		name,
	}, args...)
}

// cmdEcho simulate the echo command in shell
func cmdEcho(args ...string) *errExitCode {
	as := []any{}
	for _, arg := range args {
		as = append(as, arg)
	}
	fmt.Println(as...)
	return nil
}

// cmdPrintWithSleep prints out its arguments in lines with 10ms time gap in between
func cmdPrintWithSleep(args ...string) *errExitCode {
	for _, arg := range args {
		time.Sleep(10 * time.Millisecond)
		fmt.Println(arg)
	}

	return nil
}

// cmdErrorMaker returns exit status from args[1] and print out messages in args[2:]
func cmdErrorMaker(args ...string) *errExitCode {
	code, _ := strconv.Atoi(args[0])

	return &errExitCode{
		code:  code,
		error: strings.Join(args[1:], "\n"),
	}

}

func cmdSummaryF(args ...string) *errExitCode {
	f := args[0]
	summaryContent := args[1]
	outs := args[2:]

	err := os.WriteFile(f, []byte(summaryContent), 0644)
	if err != nil {
		return &errExitCode{
			code:  1,
			error: err.Error(),
		}
	}

	fmt.Println(strings.Join(outs, "\n"))
	return nil

}

func cmdGenerateImages(args ...string) *errExitCode {
	f := args[0]

	urls := args[1:]

	s, err := os.Stat(f)
	if err != nil && os.IsNotExist(err) {
		return &errExitCode{code: 1, error: err.Error()}
	}

	if !s.IsDir() || err != nil {
		err = os.MkdirAll(s.Name(), 0755)
		if err != nil {
			return &errExitCode{code: 1, error: err.Error()}
		}
	}

	if len(urls)%2 != 0 {
		return &errExitCode{
			code:  2,
			error: "missing url naming pairs",
		}
	}

	err = os.WriteFile(path.Join(f, "README"), []byte("readme"), 0644)
	if err != nil {
		return &errExitCode{code: 1, error: err.Error()}
	}

	for len(urls) > 0 {
		name, u := urls[0], urls[1]
		urls = urls[2:]
		img := github.CheckRunImage{
			ImageURL: github.String(u),
		}

		p := path.Join(f, name+".json")
		o, err := json.Marshal(img)
		if err != nil {
			return &errExitCode{code: 1, error: err.Error()}
		}

		err = os.WriteFile(p, o, 0644)
		if err != nil {
			return &errExitCode{code: 1, error: err.Error()}
		}
	}

	return nil
}

func cmdGenerateAnnotations(args ...string) *errExitCode {
	f := args[0]

	paths := args[1:]

	s, err := os.Stat(f)
	if err != nil && os.IsNotExist(err) {
		return &errExitCode{code: 1, error: err.Error()}
	}

	if !s.IsDir() || err != nil {
		err = os.MkdirAll(s.Name(), 0755)
		if err != nil {
			return &errExitCode{code: 1, error: err.Error()}
		}
	}

	if len(paths)%2 != 0 {
		return &errExitCode{
			code:  2,
			error: "missing path naming pairs",
		}
	}

	err = os.WriteFile(path.Join(f, "README"), []byte("readme"), 0644)
	if err != nil {
		return &errExitCode{code: 1, error: err.Error()}
	}

	for len(paths) > 0 {
		name, pa := paths[0], paths[1]
		paths = paths[2:]
		annotation := github.CheckRunAnnotation{
			Path: github.String(pa),
		}

		p := path.Join(f, name+".json")
		o, err := json.Marshal(annotation)
		if err != nil {
			return &errExitCode{code: 1, error: err.Error()}
		}

		err = os.WriteFile(p, o, 0644)
		if err != nil {
			return &errExitCode{code: 1, error: err.Error()}
		}
	}

	return nil
}

func cmdCatBigUnicode(args ...string) *errExitCode {
	builder := strings.Builder{}

	for i := 0; i < 1024*21; i++ {
		builder.WriteString("你好")
	}
	fmt.Println(builder.String(), builder.Len())
	return nil
}

func cmdRepeatSummary(args ...string) *errExitCode {
	f := args[0]
	timeStr := args[1]
	toRepeat := args[2]

	times, err := strconv.Atoi(timeStr)
	if err != nil {
		return &errExitCode{code: 1, error: err.Error()}
	}

	err = os.WriteFile(f, []byte(strings.Repeat(toRepeat, times)), 0644)
	if err != nil {
		return &errExitCode{
			code:  1,
			error: err.Error(),
		}
	}

	fmt.Println("")

	return nil
}

func cmdCaptureSignal(_ ...string) *errExitCode {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan)

	s := <-sigChan

	return &errExitCode{
		code:  1,
		error: fmt.Sprintf("capture signal: %s", s),
	}
}
