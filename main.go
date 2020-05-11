package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"sort"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/mmcdole/gofeed"
	bolt "go.etcd.io/bbolt"
)

const (
	maxAgePrintItem = time.Hour * 48
	opmlFilename    = "feeds.opml"
)

// Feed rss or atom feed with an optional replacement title
type Feed struct {
	Link     string
	AltTitle string
}

type opml struct {
	XMLName   xml.Name  `xml:"opml"`
	Version   string    `xml:"version,attr"`
	OpmlTitle string    `xml:"head>title"`
	Outlines  []outline `xml:"body>outline"`
}

type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr"`
	Type     string    `xml:"type,attr"`
	XMLURL   string    `xml:"xmlUrl,attr"`
	HTMLURL  string    `xml:"htmlUrl,attr"`
	Favicon  string    `xml:"rssfr-favicon,attr"`
	Outlines []outline `xml:"outline"`
}

// processOutline recursively process opml outlines returning all found feeds
func processOutline(outs []outline) []*Feed {
	feeds := make([]*Feed, 0)

	for _, out := range outs {
		if len(out.Outlines) > 0 {
			feeds = append(feeds, processOutline(out.Outlines)...)
		}

		if len(out.XMLURL) > 0 && len(out.Text) > 0 {
			feed := Feed{Link: out.XMLURL, AltTitle: out.Text}
			feeds = append(feeds, &feed)
		}
	}

	return feeds
}

// GetFeeds return a list of feeds found in an opml file
func GetFeeds(filename string) ([]*Feed, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var opmlDoc opml
	xml.Unmarshal(data, &opmlDoc)

	feeds := processOutline(opmlDoc.Outlines)

	return feeds, nil
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
	if i[x].Published.Before(i[y].Published) {
		return true
	}
	if i[x].Title < i[y].Title {
		return true
	}
	return i[x].FeedTitle < i[y].FeedTitle
}

// GetKey get the key that should be used for uniqely identifying this feed item suitable for use in a KV store
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

// main this is a test
func main() {
	json := jsoniter.ConfigFastest

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
	defer db.Close()

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
					b.Put([]byte(key), data)
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

	err = db.View(func(tx *bolt.Tx) error {

		// all the root items are our buckets
		// gotta catch them all!
		c := tx.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			b := tx.Bucket(k)
			cb := b.Cursor()
			for bk, bv := cb.First(); bk != nil && bv != nil; bk, bv = cb.Next() {
				var item FeedItem
				err = json.Unmarshal(bv, &item)
				if err != nil {
					log.Printf("failed to unmarshal value: %v", err)
					continue
				}
				if item.Published.After(time.Now().Add(-maxAgePrintItem)) {
					itemsToPrint = append(itemsToPrint, &item)
				}
			}
		}

		return nil
	})
	if err != nil {
		log.Fatalf("failed to create view: %v", err)
	}

	sort.Sort(sort.Reverse(sortedFeedItems(itemsToPrint)))
	for _, item := range itemsToPrint {
		fmt.Printf("[%s](%s)\n", item.Title, item.Link)
	}
}
