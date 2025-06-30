package run

// Config holds the configuration pass on from the parent command
type Config struct {
	ChecksService   ChecksService
	IsAuthenticated bool
}
