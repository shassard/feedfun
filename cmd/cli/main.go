package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/shassard/feedfun/internal/config"
	"github.com/shassard/feedfun/internal/output"
	"github.com/shassard/feedfun/internal/processing"

	"github.com/cockroachdb/pebble"
)

func oneShot(cfg *config.Config) int {
	slog.Info("running in one-shot mode")

	db, err := pebble.Open(cfg.DbDirname, &pebble.Options{})
	if err != nil {
		slog.Error("failed to open database", "error", err)
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("failed to close database", "error", err)
		}
	}()

	if !cfg.NoRefreshMode {
		if err := processing.GetFeeds(db, cfg); err != nil {
			slog.Error("failed to get feeds", "error", err)
			return 1
		}
	}

	_, err = output.WriteItems(db, cfg.OutputMode, cfg.OutputMaxAge)
	if err != nil {
		slog.Error("failed to output items", "error", err)
		return 1
	}

	return 0
}

func httpDaemon(cfg *config.Config) int {
	slog.Info("running in daemon mode")

	db, err := pebble.Open(cfg.DbDirname, &pebble.Options{})
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

	slog.Info("created refresh ticker", "duration", cfg.RefreshTicker)
	ticker := time.NewTicker(cfg.RefreshTicker)
	go func() {
		for ; true; <-ticker.C {
			start := time.Now()
			if err := processing.GetFeeds(db, cfg); err != nil {
				slog.Error("failed to get feeds on tick", "error", err)
			}
			out, err = output.WriteItems(db, cfg.OutputMode, cfg.OutputMaxAge)
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

	slog.Info("listening for http requests", "port", cfg.Daemon.Port)
	slog.Error("failed to listen and serve http", "error", http.ListenAndServe(fmt.Sprintf(":%d", cfg.Daemon.Port), nil))

	return 0
}

// main this is a test
func main() {
	var err error
	var cfg config.Config

	flag.BoolVar(&cfg.NoRefreshMode, "norefresh", false, "skip refreshing feeds on start")
	var outMode string // temporary varible to setup our mode enum
	flag.StringVar(&outMode, "outmode", "html", "set output mode to \"markdown\" or \"html\"")
	flag.StringVar(&cfg.OpmlFilename, "opml", "feeds.opml", "opml filename")
	flag.StringVar(&cfg.DbDirname, "db", "data.db", "pebble database directory name")
	var publishCutoff string
	flag.StringVar(&publishCutoff, "publishcutoff", "2d", "output articles published within this duration of time")
	flag.BoolVar(&cfg.Daemon.Mode, "daemon", false, "enable http listener daemon")
	flag.UintVar(&cfg.Daemon.Port, "port", 8173, "port which the daemon will listen on")
	var refreshTicker string
	flag.StringVar(&refreshTicker, "refreshticker", "1h", "duration to wait before refreshing feeds")
	flag.BoolVar(&cfg.Ollama.Enable, "ollamaenable", false, "enable ollama integration for article summary generation")
	flag.StringVar(&cfg.Ollama.Model, "ollamamodel", "phi3:medium", "llm to use when submitting requests to ollama")
	flag.Parse()

	cfg.OutputMaxAge, err = time.ParseDuration(publishCutoff)
	if err != nil {
		slog.Error("failed to parse publishcutoff duration", "error", err)
		os.Exit(1)
	}

	cfg.RefreshTicker, err = time.ParseDuration(refreshTicker)
	if err != nil {
		slog.Error("failed to parse refreshticker duration", "error", err)
		os.Exit(1)
	}

	switch outMode {
	case "html":
		cfg.OutputMode = output.HTMLOutputMode
	case "markdown":
		cfg.OutputMode = output.MarkdownOutputMode
	default:
		cfg.OutputMode = output.UnknownOutputMode
	}

	if cfg.Daemon.Mode {
		os.Exit(httpDaemon(&cfg))
	} else {
		os.Exit(oneShot(&cfg))
	}
}
