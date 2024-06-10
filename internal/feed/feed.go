package feed

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	jsonIter "github.com/json-iterator/go"
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
	Summary   string
	Published time.Time
}

type OllamaRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
	Prompt string `json:"prompt"`
}

type OllamaResponse struct {
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	DoneReason         string `json:"done_reason"`
	TotalDuration      uint64 `json:"total_duration"`
	LoadDuration       uint64 `json:"load_duration"`
	PromptEvalDuration uint64 `json:"prompt_eval_duration"`
	EvalCount          uint64 `json:"eval_count"`
	EvalDuration       uint64 `json:"eval_duration"`
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

func (i *Item) GenerateSummary(model string) error {
	if len(model) == 0 {
		return errors.New("no llm model was specified")
	}

	if len(i.Link) == 0 {
		return errors.New("the article contains no link to generate a summary from")
	}

	parsedURL, err := url.Parse(i.Link)
	if err != nil {
		return err
	}

	if !slices.Contains([]string{"http", "https"}, parsedURL.Scheme) {
		return errors.New("article link does not contain a supported scheme")
	}

	json := jsonIter.ConfigFastest

	slog.Debug("generating summary for article", "link", i.Link)

	prompt := fmt.Sprintf("Generate a two sentence summary of %s", i.Link)

	oReq := OllamaRequest{Model: model, Stream: false, Prompt: prompt}

	postData, err := json.Marshal(&oReq)
	if err != nil {
		return err
	}
	buf := bytes.NewReader(postData)

	resp, err := http.Post("http://localhost:11434/api/generate", "application/json", buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var oResp OllamaResponse
	if err := json.Unmarshal(body, &oResp); err != nil {
		return err
	}

	if !oResp.Done && oResp.DoneReason != "stop" {
		return fmt.Errorf("ollama did not finish: %t %s", oResp.Done, oResp.DoneReason)
	}

	i.Summary = oResp.Response

	return nil
}
