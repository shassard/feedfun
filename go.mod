module github.com/shassard/feedfun

go 1.14

require (
	github.com/PuerkitoBio/goquery v1.5.1 // indirect
	github.com/cockroachdb/pebble v0.0.0-20200611222605-93a10284c32a
	github.com/json-iterator/go v1.1.9
	github.com/mmcdole/gofeed v1.0.0
	golang.org/x/net v0.0.0-20200506145744-7e3656a0809f // indirect
	golang.org/x/text v0.3.2 // indirect
)

replace github.com/mmcdole/gofeed => github.com/shassard/gofeed v1.0.1-0.20200514035827-2d27f3b69931
