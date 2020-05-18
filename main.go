package main

import (
	"flag"
	"log"
	"time"

	"github.com/shassard/feedfun/internal/output"
	"github.com/shassard/feedfun/internal/processing"

	bolt "go.etcd.io/bbolt"
)

// main this is a test
func main() {
	var mode int

	var outMode string
	flag.StringVar(&outMode, "outmode", "markdown", "set output mode to \"markdown\" or \"html\"")

	var opmlFilename string
	flag.StringVar(&opmlFilename, "opml", "feeds.opml", "opml filename")

	var boltDBFilename string
	flag.StringVar(&boltDBFilename, "db", "data.db", "bolt database filename")

	var maxAgeHours uint
	flag.UintVar(&maxAgeHours, "hours", 48, "output articles published within this many hours")

	flag.Parse()

	switch outMode {
	case "html":
		mode = output.HTMLOutputMode
	case "markdown":
		mode = output.MarkdownOutputMode
	default:
		mode = output.UnknownOutputMode
	}

	db, err := bolt.Open(boltDBFilename, 0600, nil)
	if err != nil {
		log.Fatal("failed to open bolt database")
	}
	defer func() { _ = db.Close() }()

	if err := processing.GetFeeds(db, opmlFilename); err != nil {
		log.Fatal("failed to get feeds: %w", err)
	}

	if err := output.OutputItems(db, mode, time.Duration(int64(maxAgeHours)*int64(time.Hour))); err != nil {
		log.Fatal("failed to output items: w", err)
	}
}
