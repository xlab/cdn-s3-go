package main

import (
	"fmt"
	"log/slog"
	"os"

	"upd.dev/upd/cdn-s3-go/version"

	"github.com/valyala/fasthttp"
)

func main() {
	forceJSON := os.Getenv("CDN_LOG_FORCE_JSON")
	isInteractive := false

	if stat, err := os.Stdout.Stat(); err == nil {
		isInteractive = (stat.Mode() & os.ModeCharDevice) != 0
	}

	var handler slog.Handler
	if forceJSON == "true" || forceJSON == "1" || !isInteractive {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})

		slog.SetDefault(slog.New(handler))
	}

	if len(os.Args) < 2 {
		printBanner()
		os.Exit(0)
	}

	cmd := os.Args[1]

	switch cmd {
	case "start":
		startServer()
	case "upload":
		uploadFile()
	case "version":
		fmt.Println(version.Version())
		os.Exit(0)
	default:
		printBanner()
		os.Exit(1)
	}
}

func printBanner() {
	fmt.Println(` ██████╗██████╗ ███╗   ██╗      ███████╗██████╗ `)
	fmt.Println(`██╔════╝██╔══██╗████╗  ██║      ██╔════╝╚════██╗`)
	fmt.Println(`██║     ██║  ██║██╔██╗ ██║█████╗███████╗ █████╔╝`)
	fmt.Println(`██║     ██║  ██║██║╚██╗██║╚════╝╚════██║ ╚═══██╗`)
	fmt.Println(`╚██████╗██████╔╝██║ ╚████║      ███████║██████╔╝`)
	fmt.Println(` ╚═════╝╚═════╝ ╚═╝  ╚═══╝      ╚══════╝╚═════╝ `)
	fmt.Println()
	fmt.Println("CDN Service - Copyright (C) 2025 <xlab@upd.dev>")
	fmt.Println()
	fmt.Println("This program is free software: you can redistribute it and/or modify")
	fmt.Println("it under the terms of the GNU General Public License as published by")
	fmt.Println("the Free Software Foundation, either version 3 of the License, or")
	fmt.Println("(at your option) any later version.")
	fmt.Println()
	fmt.Println("This program is distributed in the hope that it will be useful,")
	fmt.Println("but WITHOUT ANY WARRANTY; without even the implied warranty of")
	fmt.Println("MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the")
	fmt.Println("GNU General Public License for more details.")
	fmt.Println()
	fmt.Println("You should have received a copy of the GNU General Public License")
	fmt.Println("along with this program.  If not, see <https://www.gnu.org/licenses/>.")
	fmt.Println()
	fmt.Fprintf(os.Stderr, "Usage: %s <command>\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  start    Start the CDN server\n")
	fmt.Fprintf(os.Stderr, "  upload   Upload a file to S3 bucket\n")
	fmt.Fprintf(os.Stderr, "  version  Print version information\n")
}

func startServer() {
	readEnv()

	srv, err := newServer()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	addr := os.Getenv("CDN_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	slog.Info("starting CDN server", "addr", addr)
	slog.Info("serving buckets", "buckets", srv.bucketPublicNames)
	slog.Info("region aliases", "regions", srv.regionAliases)

	if err := fasthttp.ListenAndServe(addr, srv.handleRequest); err != nil {
		slog.Error("error in ListenAndServe", "error", err)
		os.Exit(1)
	}
}
