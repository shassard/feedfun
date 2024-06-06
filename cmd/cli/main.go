package main

import (
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/shassard/feedfun/internal/output"
	"github.com/shassard/feedfun/internal/processing"

	"github.com/cockroachdb/pebble"
)

// main this is a test
func main() {
	logger := slog.Default()

	var outMode string
	flag.StringVar(&outMode, "outmode", "html", "set output mode to \"markdown\" or \"html\"")

	var opmlFilename string
	flag.StringVar(&opmlFilename, "opml", "feeds.opml", "opml filename")

	var dbFilename string
	flag.StringVar(&dbFilename, "db", "data.db", "database filename")

	var maxAgeHours uint
	flag.UintVar(&maxAgeHours, "hours", 48, "output articles published within this many hours")

	flag.Parse()

	var mode int

	switch outMode {
	case "html":
		mode = output.HTMLOutputMode
	case "markdown":
		mode = output.MarkdownOutputMode
	default:
		mode = output.UnknownOutputMode
	}

	db, err := pebble.Open(dbFilename, &pebble.Options{})
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if err := processing.GetFeeds(db, opmlFilename); err != nil {
		logger.Error("failed to get feeds", "error", err)
		os.Exit(1)
	}

	maxAge := time.Duration(int64(maxAgeHours) * int64(time.Hour))
	if err := output.WriteItems(db, mode, maxAge); err != nil {
		logger.Error("failed to output items", "error", err)
		os.Exit(1)
	}
}
