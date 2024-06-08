package output

import (
	"bytes"
	_ "embed"
	"fmt"
	"log/slog"
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
	FileMode          = 0600
	UnknownOutputMode = iota
	MarkdownOutputMode
	HTMLOutputMode
)

var ErrUnknownMode = fmt.Errorf("unknown output mode")

//go:embed style.css
var stylesheet string

// generateItemsHTML write items to disk in html format.
func generateItemsHTML(items []*f.Item) ([]byte, error) {
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

		data = append(data,
			[]byte(
				fmt.Sprintf(
					"<p><a href=\"%s\">%s</a> <small>%s @ %s</small></p>\n",
					item.Link, item.Title, item.FeedTitle, item.Published.In(loc)))...)
		if len(item.Summary) > 0 {
			data = append(data,
				[]byte(
					fmt.Sprintf(
						"<p><small>%s</small></p>\n",
						item.Summary))...)
		}

		lastItemTime = &item.Published
	}

	// footer
	data = append(data, []byte(fmt.Sprintf("<p><small>Generated: %s</small></p>", time.Now()))...)
	data = append(data, []byte("</body>\n</html>\n")...)

	return data, nil
}

// generateItemsMarkdown write items to disk in markdown format.
func generateItemsMarkdown(items []*f.Item) ([]byte, error) {
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

	return data, nil
}

// WriteItems read items from the pebble db and output them in the mode requested.
func WriteItems(db *pebble.DB, mode int, maxAge time.Duration) ([]byte, error) {
	json := jsonIter.ConfigFastest

	itemsToPrint := make([]*f.Item, 0)

	b := db.NewIndexedBatch()

	// all the root items are our buckets
	// gotta catch them all!
	i, err := b.NewIter(nil)
	if err != nil {
		return nil, err
	}

	for k := i.First(); k; k = i.Next() {
		if v := i.Value(); v != nil {

			// split the fields from our key
			key := i.Key()
			keyParts := bytes.Split(key, []byte(string(f.KeySeparator)))

			switch len(keyParts) {

			case 5: // current
				if !bytes.Equal(keyParts[0], f.KeyVerIdentifier) {
					slog.Warn("invalid key value identifier",
						"key_parts", keyParts[0])
					continue
				}

				itemPublishTime := keyParts[4]
				t, err := time.Parse(time.RFC3339, string(itemPublishTime))
				if err != nil {
					slog.Warn("failed to convert kv key publish time component",
						"key_parts", keyParts,
						"error", err)
					continue
				}

				// apply the cutoff date and collect recent items
				if t.After(time.Now().Add(-maxAge)) {
					var item f.Item
					if err := json.Unmarshal(v, &item); err != nil {
						return nil, fmt.Errorf("failed to unmarshal value: %w", err)
					}

					itemsToPrint = append(itemsToPrint, &item)
				}

			case 3: // legacy
				var item f.Item
				if err := json.Unmarshal(v, &item); err != nil {
					return nil, fmt.Errorf("failed to unmarshal value: %w", err)
				}
				// apply the cutoff date and collect recent items
				if item.Published.After(time.Now().Add(-maxAge)) {
					itemsToPrint = append(itemsToPrint, &item)
				}

			default: // wtf?
				slog.Warn("found unexpected part count",
					"parts_count", len(keyParts),
					"key", key)
			}
		}
	}

	if err := i.Close(); err != nil {
		slog.Error("error closing iterator", "error", err)
	}

	if err := b.Close(); err != nil {
		slog.Error("error closing batch", "error", err)
	}

	// newest items to the top
	sort.Sort(sort.Reverse(f.SortedFeedItems(itemsToPrint)))

	var data []byte

	switch mode {
	case HTMLOutputMode:
		data, err = generateItemsHTML(itemsToPrint)
		if err != nil {
			return nil, fmt.Errorf("failed to write items: %w", err)
		}
		if err := os.WriteFile(fmt.Sprintf("%s.html", FilenameBase), data, FileMode); err != nil {
			return nil, err
		}

	case MarkdownOutputMode:
		data, err = generateItemsMarkdown(itemsToPrint)
		if err != nil {
			return nil, fmt.Errorf("failed to write items: %w", err)
		}
		if err := os.WriteFile(fmt.Sprintf("%s.md", FilenameBase), data, FileMode); err != nil {
			return nil, err
		}

	case UnknownOutputMode:
		return nil, ErrUnknownMode
	}

	return data, nil
}
