package main

import (
	"flag"
	"log"
	"time"

	"github.com/shassard/feedfun/internal/output"
	"github.com/shassard/feedfun/internal/processing"

	"github.com/cockroachdb/pebble"
)

// main this is a test
func main() {
	var outMode string
	flag.StringVar(&outMode, "outmode", "markdown", "set output mode to \"markdown\" or \"html\"")

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

	db, err := pebble.Open(dbFilename, nil)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := processing.GetFeeds(db, opmlFilename); err != nil {
		log.Fatalf("failed to get feeds: %v", err)
	}

	if err := output.WriteItems(db, mode, time.Duration(int64(maxAgeHours)*int64(time.Hour))); err != nil {
		log.Fatalf("failed to output items: %v", err)
	}
}
