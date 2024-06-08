package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/shassard/feedfun/internal/output"
	"github.com/shassard/feedfun/internal/processing"

	"github.com/cockroachdb/pebble"
	openai "github.com/sashabaranov/go-openai"
)

type Config struct {
	daemonMode    bool
	daemonPort    uint
	dbDirname     string
	maxAgeHours   uint
	mode          int
	noRefreshMode bool
	openaiClient  *openai.Client
	openaiToken   string
	opmlFilename  string
	refreshTicker time.Duration
}

func oneShot(config *Config) int {
	slog.Info("running in one-shot mode")

	db, err := pebble.Open(config.dbDirname, &pebble.Options{})
	if err != nil {
		slog.Error("failed to open database", "error", err)
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("failed to close database", "error", err)
		}
	}()

	if !config.noRefreshMode {
		if err := processing.GetFeeds(db, config.opmlFilename, config.openaiClient); err != nil {
			slog.Error("failed to get feeds", "error", err)
			return 1
		}
	}

	maxAge := time.Duration(int64(config.maxAgeHours) * int64(time.Hour))
	_, err = output.WriteItems(db, config.mode, maxAge)
	if err != nil {
		slog.Error("failed to output items", "error", err)
		return 1
	}

	return 0
}

func httpDaemon(config *Config) int {
	slog.Info("running in daemon mode")

	maxAge := time.Duration(int64(config.maxAgeHours) * int64(time.Hour))

	db, err := pebble.Open(config.dbDirname, &pebble.Options{})
	if err != nil {
		slog.Error("failed to open database", "error", err)
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("failed to close database", "error", err)
		}
	}()

	out := []byte("generating index page. please reload in a few moments ...")

	slog.Info("created refresh ticker", "duration", config.refreshTicker)
	ticker := time.NewTicker(config.refreshTicker)
	go func() {
		for ; true; <-ticker.C {
			start := time.Now()
			if err := processing.GetFeeds(db, config.opmlFilename, config.openaiClient); err != nil {
				slog.Error("failed to get feeds on tick", "error", err)
			}
			out, err = output.WriteItems(db, config.mode, maxAge)
			if err != nil {
				slog.Error("failed to output items", "error", err)
			}
			slog.Info("refreshed feeds on tick", "took", time.Since(start))
		}
	}()

	// setup an http router and handler
	http.HandleFunc(
		"/",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, err = w.Write(out)
			if err != nil {
				slog.Error("failed to write client output", "error", err)
				return
			}
		})

	slog.Info("listening for http requests", "port", config.daemonPort)
	slog.Error("failed to listen and serve http", "error", http.ListenAndServe(fmt.Sprintf(":%d", config.daemonPort), nil))

	return 0
}

// main this is a test
func main() {
	var err error
	var config Config

	flag.BoolVar(&config.noRefreshMode, "norefresh", false, "skip refreshing feeds on start")
	var outMode string // temporary varible to setup our mode enum
	flag.StringVar(&outMode, "outmode", "html", "set output mode to \"markdown\" or \"html\"")
	flag.StringVar(&config.opmlFilename, "opml", "feeds.opml", "opml filename")
	flag.StringVar(&config.dbDirname, "db", "data.db", "pebble database directory name")
	flag.UintVar(&config.maxAgeHours, "hours", 48, "output articles published within this many hours")
	flag.BoolVar(&config.daemonMode, "daemon", false, "enable http listener daemon")
	flag.UintVar(&config.daemonPort, "port", 8173, "port which the daemon will listen on")
	var refreshTicker string
	flag.StringVar(&refreshTicker, "refreshticker", "1h", "duration to wait before refreshing feeds")
	flag.StringVar(&config.openaiToken, "openaitoken", "", "openai token for chatgpt integration")

	flag.Parse()

	if config.openaiToken != "" {
		config.openaiClient = openai.NewClient(config.openaiToken)
	}

	config.refreshTicker, err = time.ParseDuration(refreshTicker)
	if err != nil {
		slog.Error("failed to parse refreshticker duration", "error", err)
		os.Exit(1)
	}

	switch outMode {
	case "html":
		config.mode = output.HTMLOutputMode
	case "markdown":
		config.mode = output.MarkdownOutputMode
	default:
		config.mode = output.UnknownOutputMode
	}

	if config.daemonMode {
		os.Exit(httpDaemon(&config))
	} else {
		os.Exit(oneShot(&config))
	}
}
