package run

import (
	"context"
	"github.com/coder/quartz"
	"github.com/google/go-github/v64/github"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	sampleOwner      = "sampleOwner"
	sampleRepo       = "sampleRepo"
	sampleTitle      = "sampleTitle"
	sampleSummary    = "sampleSummary"
	sampleName       = "sampleName"
	sampleDetailsUrl = "sampleDetailsUrl"
	sampleHeadShA    = "sampleHeadShA"
	sampleExternalID = "sampleExternalID"
	highlight        = "bash"
)

type runConfig struct {
	Summary   string
	runId     int64
	frequency time.Duration
}

func newInMemoryChecksService(t *testing.T, runId int64) *inMemoryChecksService {
	return &inMemoryChecksService{
		CheckRuns: make([]*wrappedCheckRun, 0),
		RunId:     runId,
		lock:      &sync.Mutex{},
		t:         t,
	}
}

type inMemoryChecksService struct {
	CheckRuns []*wrappedCheckRun
	RunId     int64
	lock      *sync.Mutex
	t         *testing.T
}

func (i *inMemoryChecksService) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CreateCheckRunOptions) (*github.CheckRun, *github.Response, error) {
	i.t.Helper()
	i.lock.Lock()
	defer i.lock.Unlock()
	i.CheckRuns = append(i.CheckRuns, &wrappedCheckRun{
		Owner:    owner,
		Repo:     repo,
		RunId:    i.RunId,
		CheckRun: opts,
	})
	return &github.CheckRun{ID: github.Int64(i.RunId)}, nil, nil
}

func (i *inMemoryChecksService) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts github.UpdateCheckRunOptions) (*github.CheckRun, *github.Response, error) {
	i.t.Helper()
	i.lock.Lock()
	defer i.lock.Unlock()
	i.CheckRuns = append(i.CheckRuns, &wrappedCheckRun{
		Owner:    owner,
		Repo:     repo,
		RunId:    checkRunID,
		CheckRun: opts,
	})
	return nil, nil, nil
}

func (i *inMemoryChecksService) GetCheckRuns() []wrappedCheckRun {
	i.t.Helper()
	i.lock.Lock()
	defer i.lock.Unlock()
	out := make([]wrappedCheckRun, len(i.CheckRuns))
	for i, r := range i.CheckRuns {
		out[i] = *r
	}

	return out
}

type wrappedCheckRun struct {
	Owner    string
	Repo     string
	RunId    int64
	CheckRun interface{}
}

func newRun(t *testing.T, cfg *runConfig, args ...string) (*Run, *quartz.Mock) {
	t.Helper()
	clock := quartz.NewMock(t)
	clock.Set(time.Now())
	if cfg == nil {
		cfg = &runConfig{}
	}

	summary := sampleSummary

	if cfg.Summary != "" {
		summary = cfg.Summary
	}

	screen, err := NewSyncScreen()
	require.NoError(t, err)

	return &Run{
		Owner:           sampleOwner,
		Repository:      sampleRepo,
		Title:           sampleTitle,
		Summary:         summary,
		Name:            sampleName,
		ExternalID:      sampleExternalID,
		DetailsURL:      sampleDetailsUrl,
		CommitSHA:       sampleHeadShA,
		UpdateFrequency: cfg.frequency,
		ShellCommand:    args,
		screen:          screen,
		clock:           clock,
		checksService:   newInMemoryChecksService(t, cfg.runId),
		isAuthenticated: true,
		SyntaxHighlight: highlight,
		sigChan:         make(chan os.Signal, 1),
	}, clock
}

func getCheckServiceOutFromRun(t *testing.T, r *Run) *inMemoryChecksService {
	t.Helper()
	return r.checksService.(*inMemoryChecksService)
}

func startCommand(t *testing.T, run *Run, done chan error) {
	t.Helper()
	err := run.Run(&Config{})
	done <- err
}

// setupRunAndStart sets up the run instance, mocked clock and a done channel. And it starts the command in
// a separate goroutine
func setupRunAndStart(t *testing.T, cfg *runConfig, realCommand bool, skipTickerSetup bool, name string, args ...string) (*Run, *quartz.Mock, chan error) {
	t.Helper()
	done := make(chan error)
	cmd := command(t, name, args...)
	if realCommand {
		cmd = append([]string{name}, args...)
	}

	r, clock := newRun(t, cfg, cmd...)
	var trap *quartz.Trap
	// set up a trap for the ticker function
	if !skipTickerSetup {
		trap = clock.Trap().TickerFunc()
		defer trap.Close()
	}
	go startCommand(t, r, done)

	if !skipTickerSetup {
		// wait for the trap to be caught
		call, err := trap.Wait(context.Background())
		require.NoError(t, err)
		// then returns from the ticker func
		call.Release()
	}

	return r, clock, done
}

type checkRun struct {
	runId       int64
	text        string
	conclusion  string
	summary     *string
	images      []*github.CheckRunImage
	annotations []*github.CheckRunAnnotation
	clock       quartz.Clock
}

func getCreateCheckRunOpt(t *testing.T, cr *checkRun) wrappedCheckRun {
	t.Helper()
	s := sampleSummary
	if cr.summary != nil {
		s = *cr.summary
	}
	run := github.CreateCheckRunOptions{
		Name:       sampleName,
		HeadSHA:    sampleHeadShA,
		DetailsURL: github.String(sampleDetailsUrl),
		ExternalID: github.String(sampleExternalID),
		Status:     github.String(checksStatusInProgress),
		Output: &github.CheckRunOutput{
			Title:       github.String(sampleTitle),
			Summary:     github.String(s),
			Images:      cr.images,
			Annotations: cr.annotations,
		},
		Actions: nil,
	}

	if cr.text != "" {
		run.Output.Text = github.String(processOutput(cr.text, highlight))
	}

	if cr.conclusion != "" {
		run.Conclusion = github.String(cr.conclusion)
		run.CompletedAt = &github.Timestamp{Time: cr.clock.Now()}
		run.Status = github.String(checksStatusCompleted)
	}
	out := wrappedCheckRun{
		Owner:    sampleOwner,
		Repo:     sampleRepo,
		RunId:    cr.runId,
		CheckRun: run,
	}
	return out
}

func getUpdateCheckRunOpt(t *testing.T, cr *checkRun) wrappedCheckRun {
	t.Helper()
	s := sampleSummary
	if cr.summary != nil {
		s = *cr.summary
	}
	output := &github.CheckRunOutput{
		Title:   github.String(sampleTitle),
		Summary: github.String(s),
	}
	if cr.text != "" {
		output.Text = github.String(processOutput(cr.text, highlight))
	}
	run := github.UpdateCheckRunOptions{
		Name:       sampleName,
		DetailsURL: github.String(sampleDetailsUrl),
		ExternalID: github.String(sampleExternalID),
		Status:     github.String(checksStatusInProgress),
		Output:     output,
		Actions:    nil,
	}

	if cr.conclusion != "" {
		run.Conclusion = github.String(cr.conclusion)
		run.CompletedAt = &github.Timestamp{Time: cr.clock.Now()}
		run.Status = github.String(checksStatusCompleted)
	}
	out := wrappedCheckRun{
		Owner:    sampleOwner,
		Repo:     sampleRepo,
		RunId:    cr.runId,
		CheckRun: run,
	}

	return out
}

func TestRunEchoCommand(t *testing.T) {
	t.Parallel()
	echoText := "testing echo"
	r, clock, done := setupRunAndStart(t, &runConfig{runId: 1, frequency: 5 * time.Second}, false, false, "echo", echoText)
	defer close(done)

	_, wait := clock.AdvanceNext()
	wait.MustWait(context.Background())
	err := <-done
	require.NoError(t, err)
	chk := &checkRun{
		runId:      1,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      1,
		text:       echoText,
		conclusion: checksConclusionSuccess,
		clock:      clock,
	}
	a := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	b := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, a, b)
}

func TestCommandOutputShouldRespectEscapeSequenceForControlCharacters(t *testing.T) {
	t.Parallel()
	echoText := "starting\n\033[1A\033[K\nstuff\bff\bs"
	r, clock, done := setupRunAndStart(t, &runConfig{runId: 2, frequency: 5 * time.Second}, false, false, "echo", echoText)
	defer close(done)

	_, wait := clock.AdvanceNext()
	wait.MustWait(context.Background())
	err := <-done
	require.NoError(t, err)
	chk := &checkRun{
		runId:      2,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      2,
		text:       "\nstuffs",
		conclusion: checksConclusionSuccess,
		clock:      clock,
	}
	a := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	b := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, a, b)
}

func TestRunPrintWithSleepCommand(t *testing.T) {
	t.Parallel()
	printTexts := []string{"line 1", "line 2"}
	r, clock, done := setupRunAndStart(t, &runConfig{runId: 3, frequency: 5 * time.Second}, false, false, "prints", printTexts...)
	defer close(done)

	// To achieve half done
	for {
		time.Sleep(1 * time.Millisecond)
		str := r.screen.ReadScreen()
		if str != "" {
			break
		}
	}

	// advance to the first time break
	_, wait := clock.AdvanceNext()
	wait.MustWait(context.Background())
	chk := &checkRun{
		runId:      3,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	updateChk := &checkRun{
		runId:      3,
		text:       "line 1",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      3,
		text:       "line 1\nline 2",
		conclusion: checksConclusionSuccess,
		clock:      clock,
	}
	expected := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, updateChk),
	}
	actual := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	err := <-done
	require.Equal(t, expected, actual)
	require.NoError(t, err)
	expected = append(expected, []wrappedCheckRun{
		getUpdateCheckRunOpt(t, endCheck),
	}...)
	actual = getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, expected, actual)
}

func TestRunErrorMakerCommand(t *testing.T) {
	t.Parallel()
	errorTexts := []string{"126", "error 1", "details"}
	r, clock, done := setupRunAndStart(t, &runConfig{runId: 4, frequency: 5 * time.Second}, false, false, "errorm", errorTexts...)
	defer close(done)
	// advance to the first time break
	_, wait := clock.AdvanceNext()
	wait.MustWait(context.Background())
	//first should advance and gets the first line
	err := <-done
	require.ErrorContains(t, err, "exit status 126")
	chk := &checkRun{
		runId:      4,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      4,
		text:       "error 1\ndetails",
		conclusion: checksConclusionFailure,
		clock:      clock,
	}
	expected := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	actual := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, expected, actual)

}

func TestErrorStartingCommand(t *testing.T) {
	// this test simulate the situation where command failed to start
	// but still sends the failed check run
	t.Parallel()
	r, clock, done := setupRunAndStart(t,
		&runConfig{runId: 5, frequency: 5 * time.Second},
		true, true, "")
	defer close(done)
	//first should advance and gets the first line
	err := <-done
	require.NotNil(t, err)
	endCheck := &checkRun{
		runId:      5,
		text:       "error starting command : exec: no command",
		conclusion: checksConclusionFailure,
		clock:      clock,
	}
	expected := []wrappedCheckRun{
		getCreateCheckRunOpt(t, endCheck),
	}
	actual := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, expected, actual)
}

func TestSummaryInFile(t *testing.T) {
	f, err := os.CreateTemp("", "")
	require.NoError(t, err)
	name := f.Name()
	err = f.Close()
	require.NoError(t, err)
	t.Parallel()
	summaryStr := "summary content"
	r, clock, done := setupRunAndStart(t, &runConfig{runId: 6, frequency: 5 * time.Second, Summary: name}, false, false, "summary-f", f.Name(), summaryStr, "stdout")
	defer close(done)

	err = <-done
	require.NoError(t, err)
	emptyStr := ""
	chk := &checkRun{
		runId:      6,
		text:       "",
		conclusion: "",
		summary:    &emptyStr,
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      6,
		text:       "stdout",
		conclusion: checksConclusionSuccess,
		summary:    &summaryStr,
		clock:      clock,
	}
	a := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		//getUpdateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	b := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, a, b)
}

func TestImageLoading(t *testing.T) {
	dir, err := os.MkdirTemp("", "image")
	defer os.RemoveAll(dir)
	require.NoError(t, err)
	url1 := "https://example.com"
	url2 := "https://example.org"

	t.Parallel()
	r, clock, done := setupRunAndStart(t, &runConfig{
		runId:     7,
		frequency: 5 * time.Second,
	}, false, false,
		"gen-image", []string{
			dir,
			"z", url2,
			"b", url1,
		}...)
	defer close(done)

	err = <-done
	require.NoError(t, err)
	chk := &checkRun{
		runId:      7,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      7,
		text:       "",
		conclusion: checksConclusionSuccess,
		clock:      clock,
		images: []*github.CheckRunImage{
			{
				ImageURL: github.String(url1),
			},
			{
				ImageURL: github.String(url2),
			},
		},
	}
	a := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	b := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, a, b)
}
func TestAnnotationLoading(t *testing.T) {
	dir, err := os.MkdirTemp("", "annotations")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	path1 := filepath.Join(dir, "code.go")
	path2 := filepath.Join(dir, "code_test.go")
	path3 := filepath.Join(dir, "code_gen_test.go")

	t.Parallel()
	r, clock, done := setupRunAndStart(t, &runConfig{
		runId:     8,
		frequency: 5 * time.Second,
	}, false, false,
		"gen-annotations", []string{
			dir,
			"z", path3,
			"c", path2,
			"b", path1,
		}...)
	defer close(done)

	err = <-done
	require.NoError(t, err)
	chk := &checkRun{
		runId:      8,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      8,
		text:       "",
		conclusion: checksConclusionSuccess,
		clock:      clock,
		annotations: []*github.CheckRunAnnotation{
			{Path: github.String(path1)},
			{Path: github.String(path2)},
			{Path: github.String(path3)},
		},
	}
	a := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	b := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, a, b)
}

func TestTruncatedOutput(t *testing.T) {
	t.Parallel()
	r, clock, done := setupRunAndStart(t, &runConfig{runId: 9, frequency: 5 * time.Second}, false, false, "cat-big-uni")
	defer close(done)

	_, wait := clock.AdvanceNext()
	wait.MustWait(context.Background())
	err := <-done
	require.NoError(t, err)
	chk := &checkRun{
		runId:      9,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      9,
		text:       r.screen.ReadScreen(),
		conclusion: checksConclusionSuccess,
		clock:      clock,
	}
	a := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	b := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, a, b)

	run := (b[len(b)-1].CheckRun).(github.UpdateCheckRunOptions)
	outputText := run.GetOutput().GetText()
	require.LessOrEqual(t, len(outputText), 65535)
	truncatePrefix := "```bash\n[truncated]...\n\n"
	suffix := "\n```"
	require.True(t, strings.HasPrefix(outputText, truncatePrefix))
	txt := strings.TrimPrefix(outputText, truncatePrefix)
	txt = strings.TrimSuffix(txt, suffix)
	// limit is 65535 characters, we have wrapper format and truncated replacement
	// for 30 characters. leaving it 65505 characters space for the output
	// and one character is 3 bytes long from the cat-big-uni command.
	// hence the last 2 bytes were given up to avoid visually broken character
	require.Equal(t, 65506, len(txt))
}

func TestTruncatedSummary(t *testing.T) {
	f, err := os.CreateTemp("", "")
	require.NoError(t, err)
	name := f.Name()
	err = f.Close()
	require.NoError(t, err)
	t.Parallel()
	textToRepeat := "summary\n"
	emptySummary := ""
	summary := processSummary(strings.Repeat(textToRepeat, 8192))
	r, clock, done := setupRunAndStart(t, &runConfig{runId: 10, frequency: 5 * time.Second, Summary: name}, false, false, "repeat-summary", name, "8192", textToRepeat)
	defer close(done)

	_, wait := clock.AdvanceNext()
	wait.MustWait(context.Background())
	err = <-done
	require.NoError(t, err)
	chk := &checkRun{
		runId:      10,
		summary:    &emptySummary,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      10,
		summary:    &summary,
		text:       r.screen.ReadScreen(),
		conclusion: checksConclusionSuccess,
		clock:      clock,
	}
	a := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	b := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, a, b)

	run := (b[len(b)-1].CheckRun).(github.UpdateCheckRunOptions)
	outputSummary := run.GetOutput().GetSummary()
	require.LessOrEqual(t, len(outputSummary), 65535)
	require.True(t, strings.HasPrefix(outputSummary, truncatedTextReplacement))

}

func TestRunCaptureSignalCommand(t *testing.T) {
	t.Parallel()
	r, clock, done := setupRunAndStart(t, &runConfig{runId: 1, frequency: 5 * time.Second}, false, false, "capture-signal")
	defer close(done)

	_, wait := clock.AdvanceNext()
	wait.MustWait(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		r.sigChan <- syscall.SIGINT
	}()
	err := <-done
	require.Error(t, err)
	chk := &checkRun{
		runId:      1,
		text:       "",
		conclusion: "",
		clock:      clock,
	}
	endCheck := &checkRun{
		runId:      1,
		text:       "capture signal: interrupt",
		conclusion: checksConclusionFailure,
		clock:      clock,
	}
	a := []wrappedCheckRun{
		getCreateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, chk),
		getUpdateCheckRunOpt(t, endCheck),
	}
	b := getCheckServiceOutFromRun(t, r).GetCheckRuns()
	require.Equal(t, a, b)
}
