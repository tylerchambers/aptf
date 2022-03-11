package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// AptSource represents a single apt source.
type AptSource struct {
	ID         uuid.UUID
	URI        string
	Suite      string
	Components []string
}

// SourceFromString validates and parses an apt source string.
func SourceFromString(s string, uuidProvider func() uuid.UUID) (*AptSource, error) {
	entry := strings.Fields(s)
	// We do not support inline options.
	if strings.ContainsAny(s, "[]") {
		return nil, fmt.Errorf("inline options are not supported")
	}
	// We need at least 4 fields: name, uri, suite, component
	if len(entry) < 4 {
		return nil, fmt.Errorf("invalid source string: %s", s)
	}
	// We only support binary distributions.
	if entry[0] != "deb" {
		return nil, fmt.Errorf("only binary (deb) repositories are supported: %s", s)
	}
	// Index 1 should contain a valid URI starting with http:// or https://
	if !strings.HasPrefix(entry[1], "http://") && !strings.HasPrefix(entry[1], "https://") {
		return nil, fmt.Errorf("invalid URI (only http(s) are supported): %s", entry[1])
	}

	// Trim trailing slashes
	entry[1] = strings.TrimSuffix(entry[1], "/")

	return &AptSource{
		ID:         uuidProvider(),
		URI:        entry[1],
		Suite:      entry[2],
		Components: entry[3:],
	}, nil
}

type AptSourceRegistry struct {
	Sources  []*AptSource
	RepoURIs []string
}

// AddSource adds a source to the registry.
func (a *AptSourceRegistry) AddSource(s *AptSource) {
	a.Sources = append(a.Sources, s)
}

func (a *AptSourceRegistry) AddSources(sources []*AptSource) {
	a.Sources = append(a.Sources, sources...)
}

// RmSource removes a source from the registry.
func (a *AptSourceRegistry) RmSource(s *AptSource) {
	for i, source := range a.Sources {
		if source == s {
			a.Sources = append(a.Sources[:i], a.Sources[i+1:]...)
			return
		}
	}
}

// RmSourceByID removes a source from the registry by ID.
func (a *AptSourceRegistry) RmSourceByID(id uuid.UUID) {
	for i, source := range a.Sources {
		if source.ID == id {
			a.Sources = append(a.Sources[:i], a.Sources[i+1:]...)
			return
		}
	}
}

// GenerateRepoURIs generates a list of repo URIs from the registry source entries.
func (a *AptSourceRegistry) GenerateRepoURIs() {
	// Empty the list.
	a.RepoURIs = []string{}

	for _, s := range a.Sources {
		// for each component, generate a repo URI
		// URI format: s.URI + "/" + "dists" + "/" + s.Suite + "/" + s.Component + "/"
		// We only support amd64 binaries for now.
		for _, c := range s.Components {
			a.RepoURIs = append(a.RepoURIs, fmt.Sprintf("%s/%s/%s/%s", s.URI, "dists", s.Suite, c))
		}
	}
}

// ParseSourcesList takes a file path and parses the file as an apt sources list.
// For each line, it validates and parses the source string.
func ParseSourcesList(path string) (*AptSourceRegistry, error) {
	// Open the file
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Create a new registry
	r := &AptSourceRegistry{}

	// Read the file line by line
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// Parse the source string
		s, err := SourceFromString(scanner.Text(), func() uuid.UUID { return uuid.New() })
		if err != nil {
			return nil, err
		}
		// Add the source to the registry
		r.AddSource(s)
	}

	// Check for errors
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Generate the repo URIs
	r.GenerateRepoURIs()

	return r, nil
}

type DownloaderMessage struct {
	Message string
	Err     error
}

// DownloadManager represents a download manager.
type DownloadManager struct {
	Workers int
}

// NewDownloadManager creates a new download manager.
func NewDownloadManager(workers int) *DownloadManager {
	return &DownloadManager{
		Workers: workers,
	}
}

type DownlaodRequest struct {
	URI         string
	Destination string
}

func URLtoFilename(url string) string {
	// trim http:// or https://
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	// trim trailing slashes
	url = strings.TrimSuffix(url, "/")
	// replace slashes with underscores
	url = strings.Replace(url, "/", "_", -1)
	// replace colons with dashes
	url = strings.Replace(url, ":", "-", -1)
	return url
}

func DownloadWorker(reqs <-chan DownlaodRequest, messages chan<- DownloaderMessage) {
	for r := range reqs {
		// Download the file
		resp, err := http.Get(r.URI)
		if err != nil {
			messages <- DownloaderMessage{Message: fmt.Sprintf("failed to download %s", r.URI), Err: err}
			continue
		}

		// Create a new file
		f, err := os.Create(r.Destination)
		if err != nil {
			messages <- DownloaderMessage{Message: fmt.Sprintf("failed to create file %s", r.Destination), Err: err}
			continue
		}

		// Write the file
		_, err = io.Copy(f, resp.Body)
		if err != nil {
			if resp.Body != nil {
				resp.Body.Close()
			}
			if f != nil {
				f.Close()
			}
			messages <- DownloaderMessage{Message: fmt.Sprintf("failed to write file %s", r.Destination), Err: err}
			continue
		}
		resp.Body.Close()
		f.Close()
		messages <- DownloaderMessage{
			Message: fmt.Sprintf("downloaded %s to %s", r.URI, r.Destination),
			Err:     nil,
		}
	}
}

func (d *DownloadManager) Download(requests []DownlaodRequest, infoLog, errLog *log.Logger) {
	// Create a channel for the requests
	reqs := make(chan DownlaodRequest, len(requests))

	// Create a channel for the messages
	messages := make(chan DownloaderMessage)

	// Create a pool of workers
	for i := 0; i < d.Workers; i++ {
		go DownloadWorker(reqs, messages)
	}
	// Send the requests to the workers
	for _, request := range requests {
		reqs <- request
	}

	// Wait for the workers to process all requests
	for i := 0; i < len(requests); i++ {
		msg := <-messages
		if msg.Err != nil {
			errLog.Printf("%s: %s", msg.Message, msg.Err)
		} else {
			infoLog.Printf("%s", msg.Message)
		}
	}
}

type AptCLient struct {
	AptfDir           string
	AptSourceRegistry *AptSourceRegistry
	DownloadManager   *DownloadManager
	InfoLog           *log.Logger
	ErrLog            *log.Logger
}

func (c *AptCLient) InitTrustDir() error {
	trustDir := filepath.Join(c.AptfDir, "trust")
	err := makeDirectoryIfNotExists(trustDir)
	if err != nil {
		c.ErrLog.Printf("failed to create trust directory %s: %s", trustDir, err)
		return err
	}

	// PGP Keys we trust
	keysDir := filepath.Join(trustDir, "keys")
	err = makeDirectoryIfNotExists(keysDir)
	if err != nil {
		c.ErrLog.Printf("failed to create keys directory %s: %s", keysDir, err)
		return err
	}

	// Hashes of files we trust
	hashesDir := filepath.Join(trustDir, "hashes")
	err = makeDirectoryIfNotExists(hashesDir)
	if err != nil {
		c.ErrLog.Printf("failed to create hashes directory %s: %s", hashesDir, err)
		return err
	}

	// create a file in the hashes directory called "releases" if it doesn't exist
	releasesFile := filepath.Join(hashesDir, "releases")
	if _, err := os.Stat(releasesFile); os.IsNotExist(err) {
		f, err := os.Create(releasesFile)
		if err != nil {
			c.ErrLog.Printf("failed to create releases file %s: %s", releasesFile, err)
			return err
		}
		f.Close()
	}

	return nil
}

func (c *AptCLient) Update() error {
	c.InfoLog.Println("Updating apt sources...")
	indexDir := filepath.Join(c.AptfDir, "index")
	err := makeDirectoryIfNotExists(indexDir)
	if err != nil {
		c.ErrLog.Printf("failed to create index directory: %s", err)
		return err
	}
	c.InfoLog.Printf("generating uris")
	c.AptSourceRegistry.GenerateRepoURIs()
	reqs := []DownlaodRequest{}
	for _, repoURI := range c.AptSourceRegistry.RepoURIs {
		reqs = append(reqs, DownlaodRequest{
			URI:         fmt.Sprintf("%s/binary-amd64/Packages.gz", repoURI),
			Destination: fmt.Sprintf("%s/%s_Packages.gz", indexDir, URLtoFilename(repoURI)),
		})
	}
	c.DownloadManager.Download(reqs, c.InfoLog, c.ErrLog)
	err = ExtractIndexes(indexDir, c.InfoLog, c.ErrLog)
	if err != nil {
		c.ErrLog.Printf("failed to extract indexes: %s", err)
		return err
	}
	return nil
}

func NewAptCLient(aptfDir string, infoLog, errLog *log.Logger) *AptCLient {
	err := makeDirectoryIfNotExists(aptfDir)
	if err != nil {
		return nil
	}
	return &AptCLient{
		AptfDir:           aptfDir,
		AptSourceRegistry: &AptSourceRegistry{},
		DownloadManager:   NewDownloadManager(10),
		InfoLog:           infoLog,
		ErrLog:            errLog,
	}
}

type ExtractionMessage struct {
	Message string
	Err     error
}

// ExtractIndexes extracts all indexes in the given directory at once.
func ExtractIndexes(dir string, infoLog, errLog *log.Logger) error {
	// Get the list of files
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), "_Packages.gz") {
			continue
		}
		// Extract the index
		out, err := recreateFile(filepath.Join(dir, strings.TrimSuffix(f.Name(), ".gz")))
		if err != nil {
			errLog.Printf("failed to extract %s", f.Name())
			return err
		}
		defer out.Close()
		in, err := os.Open(filepath.Join(dir, f.Name()))
		if err != nil {
			errLog.Printf("failed to extract %s", f.Name())

			return err

		}
		defer in.Close()
		gz, err := gzip.NewReader(in)
		if err != nil {
			errLog.Printf("failed to extract %s", f.Name())
			return err

		}
		_, err = io.Copy(out, gz)
		if err != nil {
			errLog.Printf("failed to extract %s", f.Name())
			return err
		}
		infoLog.Printf("extracted %s", f.Name())
	}
	return nil
}

func makeDirectoryIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.Mkdir(path, os.ModeDir|0755)
	}
	return nil
}

func recreateFile(path string) (*os.File, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.Create(path)
	}
	os.Remove(path)
	return os.Create(path)
}

func main() {
	fmt.Println("hello, world!")
}
