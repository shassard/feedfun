// processing will read an opml file, parsing found feeds into items and adding new items to a KV store.
package processing

import (
	"fmt"
	"log/slog"
	"net/url"
	"time"

	c "github.com/shassard/feedfun/internal/config"
	f "github.com/shassard/feedfun/internal/feed"
	"github.com/shassard/feedfun/internal/opml"

	"github.com/cockroachdb/pebble"
	jsonIter "github.com/json-iterator/go"
	"github.com/mmcdole/gofeed"
)

// processFeed read a feed and emit items to itemChan.
func processFeed(feed *f.Feed, itemChan chan<- *f.Item, done chan<- bool) {
	feedURL, err := url.Parse(feed.Link)
	if err != nil {
		slog.Error("feed does not have a valid link", "feed", feed)
		done <- true
		return
	}

	if len(feedURL.Host) == 0 || len(feedURL.Scheme) == 0 {
		slog.Error("feed does not have a valid host or scheme", "feed", feed)
		done <- true
		return
	}

	fp := gofeed.NewParser()
	parsedFeed, err := fp.ParseURL(feed.Link)
	if err != nil {
		slog.Error("error processing feed", "feed", feed, "error", err)
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

		// generate reference url for the item URL based on the feed URL.
		// this will ensure that the path is not relative, and something
		// that we can feed to an llm with no other context.
		itemURL, err := feedURL.Parse(item.Link)
		if err == nil {
			slog.Debug("replaced item url", "feed_link", feed.Link, "item_link", item.Link, "item_url", itemURL.String())
			item.Link = itemURL.String()
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
func GetFeeds(db *pebble.DB, cfg *c.Config) error {
	json := jsonIter.ConfigFastest

	feeds, err := opml.GetFeedsFromOPML(cfg.OpmlFilename)
	if err != nil {
		return fmt.Errorf("unable to parse opml file: %w", err)
	}

	if len(feeds) == 0 {
		return fmt.Errorf("no feeds found in opml: %s", cfg.OpmlFilename)
	}

	var feedProcessesWaiting uint

	feedItemChan := make(chan *f.Item)
	doneChan := make(chan bool, 1)

	for _, feed := range feeds {
		feedProcessesWaiting++
		go processFeed(feed, feedItemChan, doneChan)
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
				if cfg.Ollama.Enable {
					if article.Published.After(time.Now().Add(-cfg.OutputMaxAge)) {
						slog.Info("getting summary", "link", article.Link)
						if err := article.GenerateSummary(cfg.Ollama.Model); err != nil {
							slog.Error("error generating article summary", "article", article, "error", err)
						}
					} else {
						slog.Info("skipping summary generation for article older than cutoff", "link", article.Link, "published", article.Published)
					}
				}
				data, err := json.Marshal(&article)
				if err != nil {
					slog.Error("error marshalling article", "article", article, "error", err)
					continue
				}
				if err := b.Set(key, data, nil); err != nil {
					slog.Error("error putting data", "error", err)
					continue
				}
			} else if err != nil {
				slog.Error("batch get error", "error", err)
				continue
			} else {
				if err := closer.Close(); err != nil {
					slog.Error("closer error", "error", err)
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

	if err := b.Commit(nil); err != nil {
		slog.Error("commit error", "error", err)
	}

	return nil
}

// PruneDatabase will delete entries from the KV store which have been published before maxAge
func PruneDatabase(db *pebble.DB, cfg *c.Config) error {
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
			if item.Published.Before(time.Now().Add(-cfg.Database.PruneMaxAge)) {
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
