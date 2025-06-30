package run

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/go-github/v64/github"
	"github.com/pkg/errors"
	"github.com/rivo/uniseg"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

const (
	checksStatusInProgress   = "in_progress"
	checksStatusCompleted    = "completed"
	checksConclusionSuccess  = "success"
	checksConclusionFailure  = "failure"
	outputFormat             = "```%s\n%s\n```"
	truncatedTextReplacement = "[truncated]...\n\n"
	outputLimit              = 65535
	summaryLimit             = 65535
)

// ChecksService is an interface abstraction for GitHub ChecksService
type ChecksService interface {
	CreateCheckRun(ctx context.Context, owner, repo string, opts github.CreateCheckRunOptions) (*github.CheckRun, *github.Response, error)
	UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts github.UpdateCheckRunOptions) (*github.CheckRun, *github.Response, error)
}

func (r *Run) getCheckRunOutput() (*github.CheckRunOutput, error) {
	out := &github.CheckRunOutput{
		Annotations: nil,
		Images:      nil,
	}

	if r.Title != "" {
		out.Title = github.String(r.Title)
	}

	summary, err := r.getSummary()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	out.Summary = github.String(summary)

	text := r.screen.ReadScreen()
	if text != "" {
		text = processOutput(text, r.SyntaxHighlight)
		out.Text = github.String(text)
	}

	annotations, err := r.getAnnotations()
	if err != nil {
		return nil, errors.Wrap(err, "error getting annotations")
	}
	out.Annotations = annotations

	images, err := r.getImages()
	if err != nil {
		return nil, errors.Wrap(err, "error getting images")
	}
	out.Images = images

	return out, nil
}

func (r *Run) createCheckRun(conclusion string) error {
	opt := github.CreateCheckRunOptions{
		Name:    r.Name,
		HeadSHA: r.CommitSHA,
		Status:  github.String("in_progress"),
	}

	if r.DetailsURL != "" {
		opt.DetailsURL = github.String(r.DetailsURL)
	}

	if r.ExternalID != "" {
		opt.ExternalID = github.String(r.ExternalID)
	}

	out, err := r.getCheckRunOutput()
	if err != nil {
		return errors.Wrap(err, "error getting output")
	}

	opt.Output = out

	if conclusion != "" {
		opt.Status = github.String(checksStatusCompleted)
		opt.Conclusion = github.String(conclusion)
		opt.CompletedAt = &github.Timestamp{Time: r.clock.Now()}
	}

	if r.isAuthenticated {
		checkRun, _, err := r.checksService.CreateCheckRun(context.Background(), r.Owner, r.Repository, opt)
		if err != nil {
			return errors.Wrap(err, "error creating check Run")
		}

		r.runId = checkRun.GetID()
	} else if r.Debug {
		r.runId = -1
		err = r.debug(opt)
		if err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func (r *Run) updateCheckRun(conclusion string) error {
	opt := github.UpdateCheckRunOptions{
		Name:        r.Name,
		Status:      nil,
		Conclusion:  nil,
		CompletedAt: nil,
		Output:      nil,
		Actions:     nil,
	}

	if r.DetailsURL != "" {
		opt.DetailsURL = github.String(r.DetailsURL)
	}

	if r.ExternalID != "" {
		opt.ExternalID = github.String(r.ExternalID)
	}

	out, err := r.getCheckRunOutput()
	if err != nil {
		return errors.Wrap(err, "error getting output")
	}

	opt.Output = out
	opt.Status = github.String(checksStatusInProgress)

	if conclusion != "" {
		opt.Status = github.String(checksStatusCompleted)
		opt.Conclusion = github.String(conclusion)
		opt.CompletedAt = &github.Timestamp{Time: r.clock.Now()}
	}

	if r.isAuthenticated {
		_, _, err = r.checksService.UpdateCheckRun(context.Background(), r.Owner, r.Repository, r.runId, opt)
		if err != nil {
			return errors.Wrapf(err, "error updating check Run %d", r.runId)
		}
	} else if r.Debug {
		err = r.debug(opt)
		if err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func (r *Run) debug(payload any) error {
	marshalled, err := marshal(payload)
	if err != nil {
		return errors.Wrapf(err, "error marshaling checkRun")
	}

	_, err = fmt.Fprintf(os.Stderr, "\nSending %s:%s, %s, %d, %+v\n", reflect.TypeOf(payload).Name(), r.Owner, r.Repository, r.runId, marshalled)
	if err != nil {
		return errors.Wrapf(err, "error outputing update check request")
	}
	return nil
}

func marshal(v any) (string, error) {
	o, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", errors.WithStack(err)
	}
	return string(o), nil
}

func (r *Run) getSummary() (string, error) {
	_, err := os.Stat(r.Summary)
	if err != nil {
		return processSummary(r.Summary), nil
	}

	content, err := os.ReadFile(r.Summary)
	if err != nil {
		return "", errors.Wrap(err, "error reading summary file")
	}

	return processSummary(string(content)), nil
}

func readFromDirectory[T any](dir string) ([]*T, error) {
	if s, err := os.Stat(dir); os.IsNotExist(err) || !s.IsDir() {
		return nil, nil
	}
	out := make([]*T, 0)
	err := filepath.WalkDir(dir, func(path string, info fs.DirEntry, pathErr error) error {
		if pathErr != nil {
			return errors.WithStack(pathErr)
		}
		if info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return errors.Wrap(err, "error reading file")
		}

		var item *T
		err = json.Unmarshal(content, item)
		if err != nil {
			return errors.Wrap(err, "error parsing file")
		}

		out = append(out, item)
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "error building instance")
	}

	return out, nil
}

func (r *Run) getAnnotations() ([]*github.CheckRunAnnotation, error) {
	return readFromDirectory[github.CheckRunAnnotation](r.Annotations)
}

func (r *Run) getImages() ([]*github.CheckRunImage, error) {
	return readFromDirectory[github.CheckRunImage](r.Images)
}

func processSummary(summary string) string {
	return truncateOutput(summary, summaryLimit)
}

// processOutput wraps the output in a code block
func processOutput(text string, highlight string) string {
	formatLen := len(outputFormat) - 4 + len(highlight)
	text = truncateOutput(text, outputLimit-formatLen)
	return fmt.Sprintf(outputFormat, highlight, text)
}

func truncateOutput(input string, limit int) string {
	textLen := len(input)
	if textLen < limit {
		return input
	}

	replacementTextLen := len(truncatedTextReplacement)
	inputLimit := limit - replacementTextLen

	// start point calculated backward to the input limit
	// and also make it 4 bytes further just to cater for the
	// another unicode character
	startPoint := len(input) - inputLimit - 4

	remainingText := input[startPoint:]
	state := -1
	for len(remainingText) > 0 {
		_, remainingText, _, state = uniseg.FirstGraphemeClusterInString(remainingText, state)
		if len(remainingText) <= inputLimit {
			break
		}
	}

	return truncatedTextReplacement + remainingText
}
