// processing will read an opml file, parsing found feeds into items and adding new items to a KV store.
package processing

import (
	"fmt"
	"log"
	"time"

	f "github.com/shassard/feedfun/internal/feed"
	"github.com/shassard/feedfun/internal/opml"

	"github.com/cockroachdb/pebble"
	jsonIter "github.com/json-iterator/go"
	"github.com/mmcdole/gofeed"
)

// processFeed read a feed and emit items to itemChan.
func processFeed(feed *f.Feed, itemChan chan<- *f.Item, done chan<- bool, chErr chan<- error) {
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

		article := f.Item{
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

// GetFeeds read OPML subscriptions and populate db with items.
func GetFeeds(db *pebble.DB, opmlFilename string) error {
	json := jsonIter.ConfigFastest

	feeds, err := opml.GetFeedsFromOPML(opmlFilename)
	if err != nil {
		return fmt.Errorf("unable to parse opml file: %w", err)
	}

	if len(feeds) == 0 {
		return fmt.Errorf("no feeds found in opml: %s", opmlFilename)
	}

	var feedProcessesWaiting uint

	feedItemChan := make(chan *f.Item)
	doneChan := make(chan bool, 1)
	errChan := make(chan error, 1)

	for _, feed := range feeds {
		feedProcessesWaiting++
		go processFeed(feed, feedItemChan, doneChan, errChan)
	}

	b := db.NewIndexedBatch()
GatherFeeds:
	for {
		select {
		case article := <-feedItemChan:
			key := []byte(article.GetPrefix() + article.GetKey())
			// don't put articles that are already in the store
			if _, closer, err := b.Get(key); err == pebble.ErrNotFound {
				data, err := json.Marshal(&article)
				if err != nil {
					errChan <- fmt.Errorf("error marshalling article %v: %w", article, err)
					continue
				}
				if err := b.Set(key, data, nil); err != nil {
					errChan <- fmt.Errorf("error putting data: %w", err)
					continue
				}
			} else if err != nil {
				errChan <- fmt.Errorf("batch get error: %w", err)
				continue
			} else {
				if err := closer.Close(); err != nil {
					errChan <- fmt.Errorf("closer error: %w", err)
					continue
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

	if err := b.Commit(nil); err != nil {
		log.Printf("commit error: %v", err)
	}

	return nil
}
