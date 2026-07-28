package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alex0esc/ceres/internal/bubbletea"
	"github.com/alex0esc/ceres/internal/server"
)


func initLogging(cli bool) {
	f, err := os.OpenFile(server.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}

	if cli {
		log.SetOutput(f)
	} else {
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}


func main() {
	cliMode := flag.Bool("cli", false, "starts the cli to monitor the agents")
	flag.Parse()

	// make sure errors go into log.txt and the command line if cli is off
	initLogging(*cliMode)
	
	sv, err := server.NewServer()
	if err != nil {
		log.Fatalf("could not start server: %v", err)
	}
	err = sv.StartCroneJobs()
	if err != nil {
		log.Fatalf("could not start cronejobs: %v", err)
	}

	
	if *cliMode {
		tui := bubbletea.RunTui(sv)
		tui.Wait()
	} else {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
	}
}
