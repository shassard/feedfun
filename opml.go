package main

import (
	"encoding/xml"
	"io/ioutil"
)

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
