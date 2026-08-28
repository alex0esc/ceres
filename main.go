package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alex0esc/ceres/internal/app"
	"github.com/alex0esc/ceres/internal/bubbletea"
)

//create two different writers one for the cli state and one for the tui
func initLogging() (io.Writer, io.Writer) {
	if err := os.MkdirAll(filepath.Dir(app.LogFilePath), 0755); err != nil {
		log.Fatalf("failed to create log directory: %v", err)
	}

	f, err := os.OpenFile(app.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return io.MultiWriter(f, os.Stderr), f
}

//startup sequnce for the server
func startApp() {
	err := app.Start()
	if err != nil {
		log.Fatalf("could not start app: %v", err)
	}
	slog.Info("App started succsessuflly!")
}


func main() {
	// make sure errors go into log.txt and the command line if cli is off
	cliWriter, tuiWriter := initLogging()
	log.SetOutput(cliWriter)

	// run the app
	startApp()
	
	scanner := bufio.NewScanner(os.Stdin)

	//programm loop to make it possible to reload configs and switch to tui dynamically
	for {
		if !scanner.Scan() {
			app.Shutdown()
			return
		}
		if scanner.Err() != nil {
			log.Fatalf("error while scanning console input: %v", scanner.Err())
		}

		switch scanner.Text() {
		case "tui":
			log.SetOutput(tuiWriter)
			tui := bubbletea.RunTui()
			tui.Wait()
			log.SetOutput(cliWriter)
		case "reload":
			app.Shutdown()
			startApp()
		case "exit":
			app.Shutdown()
			return
		default: 
			fmt.Println("Unkown command: only exit, tui, reload are available!")
		}	
	}
}
