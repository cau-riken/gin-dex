package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/G-Node/libgin/libgin"
	"github.com/docopt/docopt-go"
	log "github.com/sirupsen/logrus"
)

// Configuration is used to store and pass the configuration settings
// throughout the service.
type Configuration struct {
	// Port for the GIN DOI service to listen on
	Port uint16
	// The encryption key, shared with GIN Web for verification
	Key string
	// Storage location for repository data
	RepositoryStore string
	// Maximum size for text files to index
	MaxTextSize int64
	// Maximum size for PDF files to index
	MaxPDFSize int64
	// Timeout for adding contents to index (in seconds)
	Timeout int64
	// Elasticsearch server instance for querying index
	Elasticsearch *ESServer
}

func loadconfig() *Configuration {
	cfg := Configuration{}

	cfg.RepositoryStore = libgin.ReadConf("repository_store")
	cfg.Key = libgin.ReadConf("key")

	// maxTxt: Maximum size to index for text files (in MB)
	maxTxt, err := strconv.ParseInt(libgin.ReadConfDefault("text_max", "10"), 10, 64)
	if err != nil {
		log.Printf("Error while parsing maxTxt variable: %v", err)
		maxTxt = 10
		log.Printf("Using default: %d", maxTxt)
	}
	cfg.MaxTextSize = maxTxt

	// maxPDF: Maximum size to index for PDF files (in MB)
	maxPDF, err := strconv.ParseInt(libgin.ReadConfDefault("pdf_max", "100"), 10, 64)
	if err != nil {
		log.Printf("Error while parsing maxPDFize variable: %v", err)
		maxPDF = 100
		log.Printf("Using default: %d", maxPDF)
	}
	cfg.MaxPDFSize = maxPDF

	// timeout for adding contents to index (in seconds)
	timeout, err := strconv.ParseInt(libgin.ReadConfDefault("timeout", "60"), 10, 64)
	if err != nil {
		log.Printf("Error while parsing timeout variable: %v", err)
		timeout = 60
		log.Printf("Using default: %d", timeout)
	}
	cfg.Timeout = timeout

	portstr := libgin.ReadConfDefault("port", "8099")
	port, err := strconv.ParseUint(portstr, 10, 16)
	if err != nil {
		log.Printf("Error while parsing port variable: %v", err)
		port = 8099
		log.Printf("Using default: %d", port)
	}

	cfg.Port = uint16(port)

	esurl := libgin.ReadConf("elastic_url")
	els := NewESServer(esurl, "blobs", "commits", nil, nil)
	err = els.Init()
	if err != nil {
		log.Errorf("Failed to connect to elastic service: %v", err)
		os.Exit(-1)
	}

	cfg.Elasticsearch = els

	return &cfg
}

func main() {
	usage := `gin-dex.
Usage:
  gin-dex [--debug]

Options:
  --debug                         Print debug messages
`

	args, err := docopt.Parse(usage, nil, true, "gin-dex 0.3", false)
	if err != nil {
		log.Printf("Error while parsing command line: %v", err)
		os.Exit(-1)
	}

	if args["--debug"].(bool) {
		log.SetLevel(log.DebugLevel)
		log.SetFormatter(&log.TextFormatter{ForceColors: true})
	}
	log.Debug("Starting gin-dex service")

	cfg := loadconfig()

	log.Debug("Registering routes")
	http.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
		IndexH(w, r, cfg)
	})
	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		SearchH(w, r, cfg.Elasticsearch)
	})
	http.HandleFunc("/suggest", func(w http.ResponseWriter, r *http.Request) {
		SuggestH(w, r, cfg.Elasticsearch)
	})
	http.HandleFunc("/reindex", func(w http.ResponseWriter, r *http.Request) {
		ReindexH(w, r, cfg)
	})

	fmt.Printf("Listening for connections on port %d\n", cfg.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil))
}
