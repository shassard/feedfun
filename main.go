package main

import (
	"fmt"
	"log"
	"sort"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/mmcdole/gofeed"
	bolt "go.etcd.io/bbolt"
)

// Feed rss or atom feed with an optional replacement title
type Feed struct {
	URL      string
	AltTitle string
}

// FeedItem an item from a field and it's associated feed
type FeedItem struct {
	FeedTitle  string
	FeedURL    string
	Title      string
	Link       string
	Content    string
	FirstFound time.Time
}

type sortedFeedItems []*FeedItem

func (i sortedFeedItems) Len() int           { return len(i) }
func (i sortedFeedItems) Swap(x, y int)      { i[x], i[y] = i[y], i[x] }
func (i sortedFeedItems) Less(x, y int) bool { return i[x].FirstFound.Before(i[y].FirstFound) }

// GetKey get the key that should be used for uniqely identifying this feed item suitable for use in a KV store
func (i *FeedItem) GetKey() string {
	return fmt.Sprintf("%s|%s", i.FeedTitle, i.Link)
}

// ProcessFeed read a feed and emit items to itemChan
func ProcessFeed(feed Feed, itemChan chan<- *FeedItem, done chan<- bool) {
	fp := gofeed.NewParser()
	parsedFeed, _ := fp.ParseURL(feed.URL)
	for _, item := range parsedFeed.Items {
		article := FeedItem{
			FeedURL:    feed.URL,
			FeedTitle:  feed.AltTitle,
			Title:      item.Title,
			FirstFound: time.Now().UTC(),
			Link:       item.Link,
			Content:    item.Content,
		}
		itemChan <- &article
	}

	done <- true
}

// main this is a test
func main() {
	json := jsoniter.ConfigFastest

	feeds := []Feed{
		{URL: "http://rss.slashdot.org/Slashdot/slashdotMain", AltTitle: "Slashdot"},
		{URL: "http://feeds.twit.tv/twit.xml", AltTitle: "Twit"}}

	b, _ := json.Marshal(&feeds)
	log.Printf("feeds: %s", b)

	path := "data.db"
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		log.Fatal("failed to open bolt database")
	}
	defer db.Close()

	var feedCount int

	feedItemChan := make(chan *FeedItem, 1)
	doneChan := make(chan bool, 1)

	for _, feed := range feeds {
		feedCount++
		go ProcessFeed(feed, feedItemChan, doneChan)
	}

	feedItems := make([]*FeedItem, 0)

GatherFeeds:
	for {
		select {
		case article := <-feedItemChan:
			feedItems = append(feedItems, article)
		case <-doneChan:
			feedCount--
			if feedCount == 0 {
				break GatherFeeds
			}
		}
	}

	sort.Sort(sortedFeedItems(feedItems))
	for _, item := range feedItems {
		fmt.Printf("%s\n", item.GetKey())
	}
}
