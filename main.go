package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alex0esc/ceres/internal/app"
	"github.com/alex0esc/ceres/internal/bubbletea"
	"github.com/alex0esc/ceres/internal/constants"
)

//create two different writers one for the cli state and one for the tui
func initLogging() (io.Writer, io.Writer) {
	if err := os.MkdirAll(filepath.Dir(constants.LogFilePath), 0755); err != nil {
		log.Fatalf("failed to create log directory: %v", err)
	}

	f, err := os.OpenFile(constants.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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
	slog.Info("App started successuflly!")
}

func main() {
	// make sure errors go into log.txt and the command line if cli is off
	cliWriter, tuiWriter := initLogging()
	log.SetOutput(cliWriter)

	// Setup signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), 
		os.Interrupt,    // Ctrl+C
		syscall.SIGTERM, // kill command
	)
	defer stop()

	// run the app
	startApp()
	
	scanner := bufio.NewScanner(os.Stdin)

	//programm loop to make it possible to reload configs and switch to tui dynamically
	for {
		// Use a channel to wait for either input or signal
		inputCh := make(chan string, 1)
		
		go func() {
			if scanner.Scan() {
				inputCh <- scanner.Text()
			} else {
				inputCh <- "" // EOF
			}
		}()

		select {
		case <-ctx.Done():
			// Signal received (Ctrl+C or SIGTERM)
			slog.Info("Received shutdown signal")
			app.Shutdown()
			return
			
		case input := <-inputCh:
			if input == "" {
				// EOF or scanner error
				if scanner.Err() != nil {
					log.Fatalf("error while scanning console input: %v", scanner.Err())
				}
				app.Shutdown()
				return
			}

			switch input {
			case "tui":
				log.SetOutput(tuiWriter)
				err := bubbletea.RunTui()
				if err != nil {
					log.Fatalf("error could not start tui: %v", err)
				}
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
}
