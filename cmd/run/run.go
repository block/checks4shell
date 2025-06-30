package run

import (
	"context"
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/coder/quartz"
	"github.com/pkg/errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
)

// Run is the struct for the run command
type Run struct {
	Owner           string        `short:"o" env:"CHECKS4SHELL_OWNER" required:"" help:"The owner of the target GitHub repo"`
	Repository      string        `short:"r" env:"CHECKS4SHELL_REPOSITORY" required:"" help:"The target GitHub repository"`
	CommitSHA       string        `short:"c" env:"CHECKS4SHELL_COMMIT_SHA" required:"" help:"The target SHA of the check Run to be created"`
	Name            string        `short:"n" env:"CHECKS4SHELL_NAME" required:"" help:"Name of the check Run"`
	Title           string        `short:"t" env:"CHECKS4SHELL_TITLE" required:"" help:"Output title of the check"`
	DetailsURL      string        `short:"u" env:"CHECKS4SHELL_DETAILS_URL" help:"Details URL of the check" `
	ExternalID      string        `short:"e" env:"CHECKS4SHELL_EXTERNAL_ID" help:"External ID of the check" `
	Summary         string        `short:"s" env:"CHECKS4SHELL_SUMMARY" help:"Output summary of the check can either be a fixed string or a file filled with content"`
	Images          string        `short:"i" env:"CHECKS4SHELL_IMAGES" help:"Output image json directory of the check, files inside will be presented in naming order. the json structure is the same as github.CheckRunImage"`
	Annotations     string        `short:"a" env:"CHECKS4SHELL_ANNOTATIONS" help:"Output annotation directory of the check, files inside will be presented in naming order. the json structure is the same as github.CheckRunAnnotation"`
	UpdateFrequency time.Duration `short:"f" env:"CHECKS4SHELL_UPDATE_FREQUENCY" help:"Frequency to update the check run" default:"5s"`
	SyntaxHighlight string        `short:"l" env:"CHECKS4SHELL_SYNTAX_HIGHLIGHT" help:"syntax highlight you want to use for the terminal output"`
	Debug           bool          `short:"d" help:"Enable debug mode"`
	ShellCommand    []string      `arg:"" help:"Shell commands to Run and filling the check Run output text" required:""`

	screen *SyncScreen
	clock  quartz.Clock

	additionalWriters []io.Writer
	checksService     ChecksService
	runId             int64
	isAuthenticated   bool
	sigChan           chan os.Signal
}

// AfterApply will run on CLI and initialise the missing properties
func (r *Run) AfterApply(_ *kong.Context, cfg *Config) error {
	if r.screen == nil {
		screen, err := NewSyncScreen()
		if err != nil {
			return errors.WithStack(err)
		}

		r.screen = screen
	}

	if r.clock == nil {
		r.clock = quartz.NewReal()
	}

	// if there is no additional writer, add stdout
	if r.additionalWriters == nil {
		r.additionalWriters = []io.Writer{os.Stdout}
	}

	r.checksService = cfg.ChecksService
	r.isAuthenticated = cfg.IsAuthenticated

	r.sigChan = make(chan os.Signal, 1)
	signal.Notify(r.sigChan)

	return nil
}

// Run runs the command
func (r *Run) Run(cfg *Config) error {
	err := r.run()
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (r *Run) run() error {
	// setup and starts the command
	cmd := exec.Command(r.ShellCommand[0], r.ShellCommand[1:]...)
	out := io.MultiWriter(append(r.additionalWriters, r.screen)...)
	cmd.Stdout = out
	cmd.Stderr = out

	// starts the given command
	err := cmd.Start()
	if err != nil {
		var cutOffErr error
		// write the failure to screen sending to checks
		_, cutOffErr = r.screen.Write([]byte(fmt.Sprintf("error starting command %s: %v", strings.Join(r.ShellCommand, " "), err)))
		if cutOffErr != nil {
			return errors.Wrapf(cutOffErr, "Error writing update to command")
		}
		// fail the check run on application failure
		cutOffErr = r.createCheckRun(checksConclusionFailure)
		if cutOffErr != nil {
			return errors.WithStack(cutOffErr)
		}
		return errors.WithStack(err)
	}

	err = r.createCheckRun("")
	if err != nil {
		return errors.WithStack(err)
	}

	// for sending done signal
	done := make(chan error)
	// make a cancellable context for the command ticker
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		// on finishes, close the channel
		// and cancel the context so the ticker exit
		cancel()
		close(done)
	}()

	go func() {
		// given the behaviour of sub process receiving signals is unpredictable
		// the best we could do is to keep sending signals to the sub process
		for s := range r.sigChan {
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() { // no need to send to exited process
				return
			}
			sigErr := cmd.Process.Signal(s)
			// if a process has finished and slipped through the ProcessState.Exited above
			// signal send to it will return os.ErrProcessDon, in this case, just stop sending signals to it
			if sigErr != nil {
				if errors.Is(sigErr, os.ErrProcessDone) {
					return
				}
				done <- errors.Wrapf(sigErr, "error sending signals to sub process")
			}
		}
	}()

	waiter := r.clock.TickerFunc(
		ctx,
		r.UpdateFrequency,
		func() error {
			return errors.WithStack(r.updateCheckRun(""))
		},
		"read-command-output",
	)

	// wait for the ticker go routine to complete
	// this ticker will keep going on until command
	// finishes
	go func() {
		err := waiter.Wait()
		// only send communication errors other than cancellation
		if err != nil && !errors.Is(err, context.Canceled) {
			done <- errors.Wrapf(err, "error from time ticker")
		}
	}()

	// wait for the command to finish and notify the done channel
	go func() {
		err := cmd.Wait()
		if err != nil {
			done <- errors.Wrapf(err, "error finishing the command")
			return
		}
		done <- nil
	}()

	execErr := <-done
	if execErr != nil {
		err = r.updateCheckRun(checksConclusionFailure)
		if err != nil {
			return errors.Wrapf(err, "error sending last update")
		}

		return errors.WithStack(execErr)
	}

	err = r.updateCheckRun(checksConclusionSuccess)
	if err != nil {
		return errors.Wrapf(err, "error sending last update")
	}

	return nil
}
