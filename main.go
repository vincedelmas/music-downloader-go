package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--install":
			if err := installForUser(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("Installed. Find Music Downloader in your application launcher.")
			return
		case "--version":
			fmt.Println(version)
			return
		case "--help", "-h":
			fmt.Println(
				"Music Downloader\n" +
					"Launch without arguments to open the desktop app.\n" +
					"--install  Install app and launcher for the current Linux user\n" +
					"--version  Print the application version",
			)
			return
		}
	}

	runDesktop()
}
