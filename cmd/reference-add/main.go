package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/repoindex"
)

func main() {
	log.SetFlags(0)

	configPath := flag.String("config", "configs/config.json", "path to the server config used to locate the repository index")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] <git-url> [git-url...]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	urls := repositoryURLs(flag.Args())
	if len(urls) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	indexPath := cfg.RepositoryIndexPath()
	index, err := repoindex.Load(indexPath)
	if err != nil {
		log.Fatalf("load repository index: %v", err)
	}

	added, err := index.AddURLs(urls)
	if err != nil {
		log.Fatalf("add repositories: %v", err)
	}
	if err := index.Save(indexPath); err != nil {
		log.Fatalf("save repository index: %v", err)
	}

	for _, repo := range added {
		fmt.Printf("added %s %s\n", repo.Name, repo.URL)
	}
}

func repositoryURLs(args []string) []string {
	var urls []string
	for _, arg := range args {
		urls = append(urls, strings.Fields(arg)...)
	}
	return urls
}
