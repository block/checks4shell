package cmd

import (
	"context"
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/block/checks4shell/cmd/run"
	"github.com/google/go-github/v64/github"
	"github.com/jferrl/go-githubauth"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

var (
	version string
)

// Checks4shell is the parent command structure holding the GitHub App credentials
// and responsible for setting up a GitHub client for child command to use
type Checks4shell struct {
	Run                     run.Run        `cmd:"" help:"Runs the given command, updates the given Github Check Run"`
	Version                 VersionCommand `cmd:"" help:"Shows the version of the command"`
	GithubAppPrivateKey     []byte         `env:"CHECKS4SHELL_GITHUB_APP_PRIVATE_KEY" help:"Path to the private key file used to authenticate to the Github App" type:"filecontent"`
	GithubAppID             int64          `env:"CHECKS4SHELL_GITHUB_APP_ID" help:"Github App ID"`
	GithubAppInstallationId int64          `env:"CHECKS4SHELL_GITHUB_APP_INSTALLATION_ID" help:"Github App Installation ID"`
}

func (c *Checks4shell) AfterApply(ctx *kong.Context) error {
	// supplied an unauthenticated client when GitHub App credential is not supplied
	// making it easy for local testing
	if c.GithubAppPrivateKey == nil {
		ctx.Bind(&run.Config{
			ChecksService:   nil,
			IsAuthenticated: false,
		})
		return nil
	}
	// supplies authenticated client private key supplied
	appTokenSource, err := githubauth.NewApplicationTokenSource(c.GithubAppID, c.GithubAppPrivateKey)
	if err != nil {
		return errors.Wrapf(err, "error creating application token source")
	}

	installationTokenSource := githubauth.NewInstallationTokenSource(c.GithubAppInstallationId, appTokenSource)
	httpClient := oauth2.NewClient(context.Background(), installationTokenSource)
	githubClient := github.NewClient(httpClient)
	ctx.Bind(&run.Config{
		ChecksService:   githubClient.Checks,
		IsAuthenticated: true,
	})
	return nil
}

// VersionCommand is the struct for VersionCommand
type VersionCommand struct {
}

// Run prints the version for the command
func (*VersionCommand) Run() error {
	v := version
	if v == "" {
		v = "devel"
	}

	fmt.Printf("checks4shell version: %s\n", v)
	return nil
}
