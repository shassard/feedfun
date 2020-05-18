package output

import (
	"fmt"
	"io/ioutil"
	"sort"
	"time"

	f "github.com/shassard/feedfun/internal/feed"

	jsonIter "github.com/json-iterator/go"
	bolt "go.etcd.io/bbolt"
)

// output modes
const (
	UnknownOutputMode = iota
	MarkdownOutputMode
	HTMLOutputMode
)

const (
	HeaderDateFormat   = "Monday January 2, 2006"
	OutputFilenameBase = "index"
)

var ErrUnknownMode = fmt.Errorf("unknown mode")

// outputItemsHTML write items to disk in html format.
func outputItemsHTML(items []*f.FeedItem) error {
	// header
	data := []byte(
		`<html>
<head>
<link rel="stylesheet" type="text/css" href="style.css" />
<meta http-equiv="Content-Type" content="text/html; charset=UTF-8" />
<meta name="viewport" content="initial-scale=1.0" />
</head>
<body>
`)

	var lastItemTime *time.Time
	for _, item := range items {
		// check if we should print the day
		if lastItemTime == nil || (item.Published.Local().Day() != lastItemTime.Local().Day()) {
			data = append(data,
				[]byte(fmt.Sprintf("<h1>%s</h1>\n\n", item.Published.Local().Format(HeaderDateFormat)))...)
		}

		data = append(data, []byte(
			fmt.Sprintf(
				"<p><a href=\"%s\">%s</a> <small>%s @ %s</small></p>\n",
				item.Link, item.Title, item.FeedTitle, item.Published.Local()))...)

		lastItemTime = &item.Published
	}

	// footer
	data = append(data, []byte("</body>\n</html>\n")...)

	if err := ioutil.WriteFile(fmt.Sprintf("%s.html", OutputFilenameBase), data, 0600); err != nil {
		return err
	}

	return nil
}

// outputItemsMarkdown write items to disk in markdown format.
func outputItemsMarkdown(items []*f.FeedItem) error {
	var data []byte

	var lastItemTime *time.Time
	for _, item := range items {
		// check if we should print the day
		if lastItemTime == nil || (item.Published.Local().Day() != lastItemTime.Local().Day()) {
			data = append(data,
				[]byte(fmt.Sprintf("# %s\n\n", item.Published.Local().Format(HeaderDateFormat)))...)
		}

		data = append(data, []byte(
			fmt.Sprintf("[%s](%s) %s @ %s\n\n", item.Title, item.Link, item.FeedTitle, item.Published.Local()))...)

		lastItemTime = &item.Published
	}

	if err := ioutil.WriteFile(fmt.Sprintf("%s.md", OutputFilenameBase), data, 0600); err != nil {
		return err
	}

	return nil
}

// OutputItems read items from a bolt db and output them in the mode requested.
func OutputItems(db *bolt.DB, mode int, maxAge time.Duration) error {
	json := jsonIter.ConfigFastest

	itemsToPrint := make([]*f.FeedItem, 0)

	if err := db.View(func(tx *bolt.Tx) error {
		// all the root items are our buckets
		// gotta catch them all!
		c := tx.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			b := tx.Bucket(k)
			cb := b.Cursor()
			for bk, bv := cb.First(); bk != nil && bv != nil; bk, bv = cb.Next() {
				var item f.FeedItem
				if err := json.Unmarshal(bv, &item); err != nil {
					return fmt.Errorf("failed to unmarshal value: %w", err)
				}

				// apply the cutoff date and collect recent items
				if item.Published.After(time.Now().UTC().Add(-maxAge)) {
					itemsToPrint = append(itemsToPrint, &item)
				}
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to create view: %w", err)
	}

	// newest items to the top
	sort.Sort(sort.Reverse(f.SortedFeedItems(itemsToPrint)))

	switch mode {
	case HTMLOutputMode:
		if err := outputItemsHTML(itemsToPrint); err != nil {
			return fmt.Errorf("failed to write items: %w", err)
		}
	case MarkdownOutputMode:
		if err := outputItemsMarkdown(itemsToPrint); err != nil {
			return fmt.Errorf("failed to write items: %w", err)
		}
	case UnknownOutputMode:
		return ErrUnknownMode
	}

	return nil

}
