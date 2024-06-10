// processing will read an opml file, parsing found feeds into items and adding new items to a KV store.
package processing

import (
	"fmt"
	"log/slog"
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
			// no publish time, then use something stable (and old!)
			// so it doesn't keep popping up at the top of our feeds
			published = time.Unix(0, 0)
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
func GetFeeds(db *pebble.DB, opmlFilename string, generateSummaries bool, model string) error {
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
			key := article.GetKey()
			slog.Debug("article key", "key", key)

			// put articles that aren't already in the store
			if _, closer, err := b.Get(key); err == pebble.ErrNotFound {
				if generateSummaries {
					if err := article.GenerateSummary(model); err != nil {
						errChan <- fmt.Errorf("error generating article summary %v: %w", article, err)
					}
				}
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
			slog.Error("feed processing error", "error", err)

		case <-doneChan:
			feedProcessesWaiting--
			// continue waiting for items until all feed processing go routines have reported finished
			if feedProcessesWaiting == 0 {
				break GatherFeeds
			}
		}
	}

	if err := b.Commit(nil); err != nil {
		slog.Error("commit error", "error", err)
	}

	return nil
}

// PruneDatabase will delete entries from the KV store which have been published before maxAge
func PruneDatabase(db *pebble.DB, maxAge time.Duration) error {
	json := jsonIter.ConfigFastest

	i, err := db.NewIter(nil)

	if err != nil {
		return err
	}

	var retErr error
	for k := i.First(); k; k = i.Next() {
		if v := i.Value(); v != nil {
			var item f.Item
			if err := json.Unmarshal(v, &item); err != nil {
				slog.Warn("failed to unmarshal value for item",
					"value", v,
					"key", i.Key(),
					"error", err)
				retErr = err
				continue
			}

			// apply the cutoff date and collect recent items
			if item.Published.Before(time.Now().Add(-maxAge)) {
				if err := db.Delete(i.Key(), nil); err != nil {
					slog.Warn("failed to delete key",
						"key", i.Key(),
						"error", err)
					retErr = err
					continue
				}
			}
		}
	}

	return retErr
}
