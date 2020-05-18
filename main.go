package main

import (
	"flag"
	"log"

	"github.com/shassard/feedfun/internal/output"
	"github.com/shassard/feedfun/internal/processing"

	bolt "go.etcd.io/bbolt"
)

// main this is a test
func main() {
	var mode int

	var outMode string
	flag.StringVar(&outMode, "outmode", "markdown", "output mode: [markdown|html]")

	var opmlFilename string
	flag.StringVar(&opmlFilename, "opml", "feeds.opml", "opml filename")

	var boltDBFilename string
	flag.StringVar(&boltDBFilename, "db", "data.db", "bolt database filename")

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

	if err := output.OutputItems(db, mode); err != nil {
		log.Fatal("failed to output items: w", err)
	}
}
