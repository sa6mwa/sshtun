package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sa6mwa/sshtun"
)

func EditConfig(configJson string) error {
	if !IsUnixTerminal(os.Stdin) {
		return ErrNotATerminal
	}
	executables := []string{}

	envEditor := os.Getenv("EDITOR")
	switch {
	case envEditor != "" && fileExists(envEditor):
		// Use editor in the EDITOR environment variables
		executables = append(executables, envEditor)
	case editor == "":
		executables = append(executables, DefaultEditors...)
	default:
		if !fileExists(editor) {
			return ErrEditorNotFound
		}
		executables = append(executables, editor)
	}
	if len(executables) == 0 {
		return ErrNoEditorFound
	}

	tempfile, err := copyFileToTemp(configJson)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
	defer func() {
		os.Remove(tempfile)
		signal.Stop(sigCh)
		close(sigCh)
	}()

	for {
		if err := tryExec(ctx, executables, tempfile); err != nil {
			return err
		}

		if ctx.Err() != nil {
			break
		}

		marshalledConfig, err := os.ReadFile(tempfile)
		if err != nil {
			return err
		}

		var config sshtun.Tunnels
		if err := json.Unmarshal(marshalledConfig, &config); err != nil {
			fmt.Printf("Error encoding json: %v\n", err)
			fmt.Printf("Edit file again? [Y/n] ")
		retryQuestion:
			s := bufio.NewScanner(os.Stdin)
			s.Scan()
			if ctx.Err() != nil {
				break
			}
			switch {
			case s.Text() == "", strings.EqualFold(s.Text(), "y"), strings.EqualFold(s.Text(), "yes"):
				continue
			case strings.EqualFold(s.Text(), "n"), strings.EqualFold(s.Text(), "no"):
				return err
			default:
				fmt.Printf("Sorry, please answer yes or no. Edit file again? [Y/n] ")
				goto retryQuestion
			}
		}

		// Store config

		outputf, err := os.OpenFile(configJson, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if err != nil {
			return err
		}
		defer outputf.Close()
		enc := json.NewEncoder(outputf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(&config); err != nil {
			return err
		}
		break
	}
	return nil
}
