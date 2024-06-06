package output

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"time"

	f "github.com/shassard/feedfun/internal/feed"

	"github.com/cockroachdb/pebble"
	jsonIter "github.com/json-iterator/go"
)

const (
	HeaderDateFormat  = "Monday January 2, 2006"
	FilenameBase      = "index"
	UnknownOutputMode = iota
	MarkdownOutputMode
	HTMLOutputMode
)

var ErrUnknownMode = fmt.Errorf("unknown output mode")

//go:embed style.css
var stylesheet string

// outputItemsHTML write items to disk in html format.
func outputItemsHTML(items []*f.Item) error {
	// header
	data := []byte(
		fmt.Sprintf(`<html>
<head>
<title>Feeds</title>
<style>%s</style>
<meta http-equiv="Content-Type" content="text/html; charset=UTF-8" />
<meta name="viewport" content="initial-scale=1.0" />
</head>
<body>
`, stylesheet))
	loc := time.Now().Location()

	var lastItemTime *time.Time
	for _, item := range items {
		// check if we should print the day
		if lastItemTime == nil || (item.Published.Local().Day() != lastItemTime.Local().Day()) {
			data = append(data,
				[]byte(fmt.Sprintf("<h1>%s</h1>\n\n", item.Published.In(loc).Format(HeaderDateFormat)))...)
		}

		data = append(data, []byte(
			fmt.Sprintf(
				"<p><a href=\"%s\">%s</a> <small>%s @ %s</small></p>\n",
				item.Link, item.Title, item.FeedTitle, item.Published.In(loc)))...)

		lastItemTime = &item.Published
	}

	// footer
	data = append(data, []byte("</body>\n</html>\n")...)

	if err := os.WriteFile(fmt.Sprintf("%s.html", FilenameBase), data, 0600); err != nil {
		return err
	}

	return nil
}

// outputItemsMarkdown write items to disk in markdown format.
func outputItemsMarkdown(items []*f.Item) error {
	var data []byte

	loc := time.Now().Location()

	var lastItemTime *time.Time
	for _, item := range items {
		// check if we should print the day
		if lastItemTime == nil || (item.Published.In(loc).Day() != lastItemTime.In(loc).Day()) {
			data = append(data,
				[]byte(fmt.Sprintf("# %s\n\n", item.Published.In(loc).Format(HeaderDateFormat)))...)
		}

		data = append(data, []byte(
			fmt.Sprintf("[%s](%s) %s @ %s\n\n", item.Title, item.Link, item.FeedTitle, item.Published.In(loc)))...)

		lastItemTime = &item.Published
	}

	if err := os.WriteFile(fmt.Sprintf("%s.md", FilenameBase), data, 0600); err != nil {
		return err
	}

	return nil
}

// WriteItems read items from the pebble db and output them in the mode requested.
func WriteItems(db *pebble.DB, mode int, maxAge time.Duration) error {
	json := jsonIter.ConfigFastest

	itemsToPrint := make([]*f.Item, 0)

	b := db.NewIndexedBatch()

	// all the root items are our buckets
	// gotta catch them all!
	i, err := b.NewIter(nil)
	if err != nil {
		return err
	}

	for k := i.First(); k; k = i.Next() {
		if v := i.Value(); v != nil {

			var item f.Item
			if err := json.Unmarshal(v, &item); err != nil {
				return fmt.Errorf("failed to unmarshal value: %w", err)
			}

			// apply the cutoff date and collect recent items
			if item.Published.After(time.Now().Add(-maxAge)) {
				itemsToPrint = append(itemsToPrint, &item)
			}
		}
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
