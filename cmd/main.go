package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"envcheck/internal/collector"
	"envcheck/internal/server"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	port := flag.Int("port", 8484, "HTTP server port")
	interval := flag.Duration("interval", 30*time.Second, "Scan interval (e.g. 30s, 1m, 5m)")
	oneshot := flag.Bool("json", false, "Print JSON report and exit (no server)")
	flag.Parse()

	// One-shot mode: just print JSON and exit
	if *oneshot {
		c := collector.New()
		report := c.Collect()
		data, _ := c.GetReportJSON()
		_ = report
		fmt.Println(string(data))
		os.Exit(0)
	}

	// Server mode
	webFS, err := server.SubFS(webFiles, "web")
	if err != nil {
		log.Fatalf("Failed to load web files: %v", err)
	}

	srv := server.New(server.Config{
		Port:     *port,
		Interval: *interval,
		WebFS:    webFS,
	})

	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
