package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alex0esc/ceres/internal/bubbletea"
	"github.com/alex0esc/ceres/internal/server"
)

//create two different writers one for the cli state and one for the tui
func initLogging() (io.Writer, io.Writer) {
	if err := os.MkdirAll(filepath.Dir(server.LogFilePath), 0755); err != nil {
		log.Fatalf("failed to create log directory: %v", err)
	}

	f, err := os.OpenFile(server.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return io.MultiWriter(f, os.Stderr), f
}

//startup sequnce for the server
func startServer() *server.Server {
	sv, err := server.NewServer()
	if err != nil {
		log.Fatalf("could not load server: %v", err)
	}
	err = sv.Initialize()
	if err != nil {
		log.Fatalf("could not initialize server: %v", err)
	}
	slog.Info("Server started succsessuflly!")
	return sv
}


func main() {
	// make sure errors go into log.txt and the command line if cli is off
	cliWriter, tuiWriter := initLogging()
	log.SetOutput(cliWriter)
	
	scanner := bufio.NewScanner(os.Stdin)
	sv := startServer()		

	//programm loop to make it possible to reload configs and switch to tui dynamically
	for {
		scanner.Scan()
		if scanner.Err() != nil {
			log.Fatalf("rror while scanning console input: %v", scanner.Err())
		}

		switch scanner.Text() {
		case "exit":
			sv.Shutdown()
			return
		case "tui":
			log.SetOutput(tuiWriter)
			tui := bubbletea.RunTui(sv)
			tui.Wait()
			log.SetOutput(cliWriter)
		case "reload":
			sv.Shutdown()
			sv = startServer()	
		default: 
			fmt.Println("Unkown command: only exit, tui, reload are available!")
		}	
	}
}
