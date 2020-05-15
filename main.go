package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"time"

	jsonIter "github.com/json-iterator/go"
	"github.com/mmcdole/gofeed"
	bolt "go.etcd.io/bbolt"
)

const (
	maxAgePrintItem    = time.Hour * 48
	opmlFilename       = "feeds.opml"
	outputFilenameBase = "index"
	headerDateFormat   = "Monday January 2, 2006"
)

// output modes
const (
	UnknownOutputMode = iota
	MarkdownOutputMode
	HTMLOutputMode
)

// Feed rss or atom feed with an optional replacement title
type Feed struct {
	Link     string
	AltTitle string
}

// FeedItem an item from a field and it's associated feed
type FeedItem struct {
	FeedTitle string
	FeedURL   string
	Title     string
	Link      string
	Content   string
	Published time.Time
}

// sortedFeedItems utility functions to sort a list of FeedItem
type sortedFeedItems []*FeedItem

func (i sortedFeedItems) Len() int      { return len(i) }
func (i sortedFeedItems) Swap(x, y int) { i[x], i[y] = i[y], i[x] }
func (i sortedFeedItems) Less(x, y int) bool {
	if i[x].Published.Equal(i[y].Published) {
		return i[x].Title < i[y].Title
	}
	return i[x].Published.Before(i[y].Published)
}

// GetKey get the key that should be used for uniquely identifying this feed item suitable for use in a KV store
func (i *FeedItem) GetKey() string {
	return fmt.Sprintf("%s|%s", i.FeedTitle, i.Link)
}

// ProcessFeed read a feed and emit items to itemChan
func ProcessFeed(feed *Feed, itemChan chan<- *FeedItem, done chan<- bool) {
	fp := gofeed.NewParser()
	parsedFeed, err := fp.ParseURL(feed.Link)
	if err != nil {
		log.Printf("error processing feed: %+v %v", feed, err)
		done <- true
		return
	}
	for _, item := range parsedFeed.Items {

		// try to set the feed title to something nice
		var feedTitle = feed.AltTitle
		if len(feedTitle) == 0 {
			feedTitle = parsedFeed.Title
		}

		var published time.Time
		if item.PublishedParsed != nil {
			published = *item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			published = *item.UpdatedParsed
		} else {
			published = time.Now().UTC()
		}

		article := FeedItem{
			FeedURL:   feed.Link,
			FeedTitle: feedTitle,
			Title:     item.Title,
			Published: published,
			Link:      item.Link,
			Content:   item.Content,
		}
		itemChan <- &article
	}

	done <- true
}

func printItemsHTML(items []*FeedItem) error {
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
				[]byte(fmt.Sprintf("<h1>%s</h1>\n\n", item.Published.Local().Format(headerDateFormat)))...)
		}

		data = append(data, []byte(
			fmt.Sprintf(
				"<p><a href=\"%s\">%s</a> <small>%s @ %s</small></p>\n",
				item.Link, item.Title, item.FeedTitle, item.Published.Local()))...)

		lastItemTime = &item.Published
	}

	// footer
	data = append(data, []byte("</body>\n</html>\n")...)

	if err := ioutil.WriteFile(fmt.Sprintf("%s.html", outputFilenameBase), data, 0600); err != nil {
		return err
	}

	return nil
}

func printItemsMarkdown(items []*FeedItem) error {
	var data []byte

	var lastItemTime *time.Time
	for _, item := range items {
		// check if we should print the day
		if lastItemTime == nil || (item.Published.Local().Day() != lastItemTime.Local().Day()) {
			data = append(data,
				[]byte(fmt.Sprintf("# %s\n\n", item.Published.Local().Format(headerDateFormat)))...)
		}

		data = append(data, []byte(
			fmt.Sprintf("[%s](%s) %s @ %s\n\n", item.Title, item.Link, item.FeedTitle, item.Published.Local()))...)

		lastItemTime = &item.Published
	}

	if err := ioutil.WriteFile(fmt.Sprintf("%s.md", outputFilenameBase), data, 0600); err != nil {
		return err
	}

	return nil
}

// main this is a test
func main() {
	json := jsonIter.ConfigFastest

	mode := MarkdownOutputMode
	for _, arg := range os.Args {
		switch arg {
		case "-html":
			mode = HTMLOutputMode
		}
	}

	feeds, err := GetFeeds(opmlFilename)
	if err != nil {
		log.Fatalf("unable to parse opml file: %v", err)
	}

	if len(feeds) == 0 {
		log.Fatalf("no feeds found in opml: %s", opmlFilename)
	}

	path := "data.db"
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		log.Fatal("failed to open bolt database")
	}
	defer func() { _ = db.Close() }()

	var feedProcessesWaiting uint

	feedItemChan := make(chan *FeedItem)
	doneChan := make(chan bool, 1)

	for _, feed := range feeds {
		feedProcessesWaiting++
		go ProcessFeed(feed, feedItemChan, doneChan)
	}

	err = db.Batch(func(tx *bolt.Tx) error {
	GatherFeeds:
		for {
			select {
			case article := <-feedItemChan:
				b, err := tx.CreateBucketIfNotExists([]byte(article.FeedURL))
				if err != nil {
					log.Fatalf("could not create bucket: %v", err)
				}

				key := []byte(article.GetKey())
				// don't put articles that are already in the store
				if b.Get(key) == nil {
					data, err := json.Marshal(&article)
					if err != nil {
						log.Printf("error marshalling article: %v", article)
						continue
					}
					if err := b.Put(key, data); err != nil {
						log.Printf("error putting data: %v", err)
						continue
					}
				}

			case <-doneChan:
				feedProcessesWaiting--
				// continue waiting for items until all feed processing go routines have reported finished
				if feedProcessesWaiting == 0 {
					break GatherFeeds
				}
			}
		}

		return nil
	})

	itemsToPrint := make([]*FeedItem, 0)

	if err := db.View(func(tx *bolt.Tx) error {
		// all the root items are our buckets
		// gotta catch them all!
		c := tx.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			b := tx.Bucket(k)
			cb := b.Cursor()
			for bk, bv := cb.First(); bk != nil && bv != nil; bk, bv = cb.Next() {
				var item FeedItem
				if err := json.Unmarshal(bv, &item); err != nil {
					log.Printf("failed to unmarshal value: %v", err)
					continue
				}

				// apply the cutoff date and collect recent items
				if item.Published.After(time.Now().UTC().Add(-maxAgePrintItem)) {
					itemsToPrint = append(itemsToPrint, &item)
				}
			}
		}

		return nil
	}); err != nil {
		log.Fatalf("failed to create view: %v", err)
	}

	// newest items to the top
	sort.Sort(sort.Reverse(sortedFeedItems(itemsToPrint)))

	switch mode {
	case HTMLOutputMode:
		if err := printItemsHTML(itemsToPrint); err != nil {
			log.Fatalf("failed to write items: %v", err)
		}
	case MarkdownOutputMode:
		if err := printItemsMarkdown(itemsToPrint); err != nil {
			log.Fatalf("failed to write items: %v", err)
		}
	case UnknownOutputMode:
		log.Fatal("unknown output mode")
	}

}
