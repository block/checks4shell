package run

import (
	"github.com/buildkite/terminal-to-html/v3"
	"github.com/pkg/errors"
	"sync"
)

// NewSyncScreen initiate a new instance of SyncScreen
func NewSyncScreen(opts ...terminal.ScreenOption) (*SyncScreen, error) {
	screen, err := terminal.NewScreen(opts...)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &SyncScreen{
		Screen: screen,
		Lock:   &sync.RWMutex{},
	}, nil
}

// SyncScreen wraps the terminal.Screen read write with a RWLock
type SyncScreen struct {
	Screen *terminal.Screen
	Lock   *sync.RWMutex
}

// ReadScreen returns the AsPlainText from the wrapped screen
func (t *SyncScreen) ReadScreen() string {
	t.Lock.RLock()
	defer t.Lock.RUnlock()
	return t.Screen.AsPlainText()
}

// Write writes the given bytes into the screen
func (t *SyncScreen) Write(p []byte) (n int, err error) {
	t.Lock.Lock()
	defer t.Lock.Unlock()
	return t.Screen.Write(p)
}
