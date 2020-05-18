package processing

import (
	"fmt"
	"log"
	"time"

	"github.com/mmcdole/gofeed"
	f "github.com/shassard/feedfun/internal/feed"
	"github.com/shassard/feedfun/internal/opml"

	jsonIter "github.com/json-iterator/go"
	bolt "go.etcd.io/bbolt"
)

// processFeed read a feed and emit items to itemChan.
func processFeed(feed *f.Feed, itemChan chan<- *f.FeedItem, done chan<- bool, chErr chan<- error) {
	fp := gofeed.NewParser()
	parsedFeed, err := fp.ParseURL(feed.Link)
	if err != nil {
		chErr <- fmt.Errorf("error processing feed: %+v %v", feed, err)
		done <- true
		return
	}

	for _, item := range parsedFeed.Items {

		// try to set the feed title to something nice
		var feedTitle = feed.TitleOverride
		if len(feedTitle) == 0 {
			feedTitle = parsedFeed.Title
		}

		var published time.Time
		if item.PublishedParsed != nil {
			published = *item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			published = *item.UpdatedParsed
		} else {
			published = time.Now()
		}

		article := f.FeedItem{
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

// GetFeeds read opml subscriptions and populate bolt db with items.
func GetFeeds(db *bolt.DB, opmlFilename string) error {
	json := jsonIter.ConfigFastest

	feeds, err := opml.GetFeedsFromOPML(opmlFilename)
	if err != nil {
		return fmt.Errorf("unable to parse opml file: %w", err)
	}

	if len(feeds) == 0 {
		return fmt.Errorf("no feeds found in opml: %s", opmlFilename)
	}

	var feedProcessesWaiting uint

	feedItemChan := make(chan *f.FeedItem)
	doneChan := make(chan bool, 1)
	errChan := make(chan error, 1)

	for _, feed := range feeds {
		feedProcessesWaiting++
		go processFeed(feed, feedItemChan, doneChan, errChan)
	}

	if err := db.Batch(func(tx *bolt.Tx) error {
	GatherFeeds:
		for {
			select {
			case article := <-feedItemChan:
				b, err := tx.CreateBucketIfNotExists([]byte(article.FeedURL))
				if err != nil {
					return fmt.Errorf("could not create bucket: %w", err)
				}

				key := []byte(article.GetKey())
				// don't put articles that are already in the store
				if b.Get(key) == nil {
					data, err := json.Marshal(&article)
					if err != nil {
						return fmt.Errorf("error marshalling article %v: %w", article, err)
					}
					if err := b.Put(key, data); err != nil {
						return fmt.Errorf("error putting data: %w", err)
					}
				}

			case err := <-errChan:
				log.Printf("%s", err)

			case <-doneChan:
				feedProcessesWaiting--
				// continue waiting for items until all feed processing go routines have reported finished
				if feedProcessesWaiting == 0 {
					break GatherFeeds
				}
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to create batch: %w", err)
	}

	return nil
}
