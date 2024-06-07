package opml

import (
	"encoding/xml"
	"os"

	"github.com/shassard/feedfun/internal/feed"
)

type OPML struct {
	XMLName   xml.Name  `xml:"opml"`
	Version   string    `xml:"version,attr"`
	OpmlTitle string    `xml:"head>title"`
	Outlines  []Outline `xml:"body>outline"`
}

type Outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr"`
	Type     string    `xml:"type,attr"`
	XMLURL   string    `xml:"xmlUrl,attr"`
	HTMLURL  string    `xml:"htmlUrl,attr"`
	Favicon  string    `xml:"rssfr-favicon,attr"`
	Outlines []Outline `xml:"outline"`
}

// processOutline recursively process OPML outlines returning all found feeds
func processOutline(outs []Outline) []*feed.Feed {
	feeds := make([]*feed.Feed, 0)

	for _, out := range outs {
		if len(out.Outlines) > 0 {
			feeds = append(feeds, processOutline(out.Outlines)...)
		}

		if len(out.XMLURL) > 0 && len(out.Text) > 0 {
			f := feed.Feed{Link: out.XMLURL, TitleOverride: out.Text}
			feeds = append(feeds, &f)
		}
	}

	return feeds
}

// GetFeedsFromOPML return a list of feeds found in an OPML file
func GetFeedsFromOPML(filename string) ([]*feed.Feed, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var opmlDoc OPML
	if err := xml.Unmarshal(data, &opmlDoc); err != nil {
		return nil, err
	}

	feeds := processOutline(opmlDoc.Outlines)

	return feeds, nil
}
