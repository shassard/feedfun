package feed

import (
	"fmt"
	"time"
)

const (
	KeySeparator = '|'
)

// Feed rss or atom feed with an optional replacement title.
type Feed struct {
	Link          string
	TitleOverride string
}

// Item an item from a feed and the feed it was parsed from.
type Item struct {
	FeedTitle string
	FeedURL   string
	Title     string
	Link      string
	Content   string
	Published time.Time
}

// sortedFeedItems utility functions to sort a list of Item.
type SortedFeedItems []*Item

func (i SortedFeedItems) Len() int      { return len(i) }
func (i SortedFeedItems) Swap(x, y int) { i[x], i[y] = i[y], i[x] }
func (i SortedFeedItems) Less(x, y int) bool {
	if i[x].Published.Equal(i[y].Published) {
		return i[x].Title < i[y].Title
	}
	return i[x].Published.Before(i[y].Published)
}

// GetPrefix get the prefix for the key for storage to the KV store.
func (i *Item) GetPrefix() string {
	return fmt.Sprintf("%s%c", i.FeedURL, KeySeparator)
}

// GetKey get the key that should be used for uniquely identifying this feed item suitable for use in a KV store.
func (i *Item) GetKey() string {
	return fmt.Sprintf("%s%c%s", i.FeedTitle, KeySeparator, i.Link)
}
