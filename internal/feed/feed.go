package feed

import (
	"bytes"
	"time"
)

var (
	KeySeparator     = []byte("|")
	KeyVerIdentifier = []byte("v2")
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

// GetKey get the key that should be used for uniquely identifying this feed item suitable for use in a KV store.
func (i *Item) GetKey() []byte {
	return bytes.Join(
		[][]byte{
			KeyVerIdentifier,
			[]byte(i.FeedURL),
			[]byte(i.FeedTitle),
			[]byte(i.Link),
			[]byte(i.Published.Format(time.RFC3339)),
		},
		KeySeparator)
}
