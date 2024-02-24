package main

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/474420502/gcurl"
	hv "github.com/SpectoLabs/hoverfly/core"
	v2 "github.com/SpectoLabs/hoverfly/core/handlers/v2"
	"github.com/SpectoLabs/hoverfly/core/matching"
	"github.com/SpectoLabs/hoverfly/core/modes"
	log "github.com/sirupsen/logrus"
)

//go:embed "simulations/*.json"
var SimFS embed.FS

func main() {
	fmt.Println("type your curl command to be tested against your simulations: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	input := scanner.Text()
	curl := gcurl.Parse(input)
	tp := curl.CreateTemporary(curl.CreateSession())
	req, err := tp.BuildRequest()
	if err != nil {
		fmt.Println(err)
		return
	}

	hoverflyServer := confHoverfly()
	if err := importSim(hoverflyServer); err != nil {
		fmt.Println(err)
		return

	}
	if err = hoverflyServer.StartProxy(); err != nil {
		fmt.Println(err)
		return
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// starting admin interface, this is blocking
		adminApi := hv.AdminApi{}
		adminApi.StartAdminInterface(hoverflyServer)
	}()

	hoverflyURL := &url.URL{
		Host: fmt.Sprintf("%s:%s", hoverflyServer.Cfg.ListenOnHost, hoverflyServer.Cfg.ProxyPort),
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(hoverflyURL),
		},
	}

	resp, err := httpClient.Do(req)
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	bodyString := string(bodyBytes)
	fmt.Println(bodyString)

	return
}

//curl -X GET "http://httpbin.org/anything/1" -H "accept: application/json"

func confHoverfly() *hv.Hoverfly {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(log.PanicLevel)
	hoverfly := hv.NewHoverfly()
	cfg := hv.InitSettings()
	cfg.ClientAuthenticationDestination = ""
	cfg.PACFile = nil

	cfg.ResponsesBodyFilesPath = ""
	cfg.ResponsesBodyFilesAllowedOrigins = []string{}
	cfg.DisableCache = false
	cfg.CacheSize = 1000
	hoverfly.StoreLogsHook.LogsLimit = 1000
	hoverfly.Journal.EntryLimit = 1000

	cfg.ListenOnHost = "0.0.0.0"

	cfg.Webserver = false
	hoverfly.Cfg = cfg
	hoverfly.CacheMatcher = matching.CacheMatcher{
		Webserver: cfg.Webserver,
	}

	// setting mode
	if err := hoverfly.SetModeWithArguments(v2.ModeView{
		Mode:      modes.Simulate,
		Arguments: v2.ModeArgumentsView{Stateful: true},
	}); err != nil {
		fmt.Println(err)
		return nil
	}
	hoverfly.HTTP = http.DefaultClient

	return hoverfly
}

type tmpFile struct {
	path string
	name string
}

func importSim(hoverfly *hv.Hoverfly) error {
	fmt.Println("loading simulations")

	tempDir, err := os.MkdirTemp("", "simulations")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempDir) // Clean up the temporary directory after usage

	var paths []tmpFile
	fs.WalkDir(SimFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}
		paths = append(paths, tmpFile{path: path, name: d.Name()})

		return nil
	})

	fmt.Println("multiple simulation found, which one do you want to use : (* for all of them):")
	fmt.Println("*: all")
	for i, path := range paths {
		fmt.Printf("%d: %s\n", i, path)
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()

	input := scanner.Text()

	if input == "*" {
		fmt.Println("importing all simulations")
		for _, path := range paths {
			if err := importSimulation(hoverfly, tempDir, path); err != nil {
				return err
			}
		}
	} else {
		i := 0
		_, err := fmt.Sscan(input, &i)
		if err != nil {
			return err
		}
		fmt.Println("importing simulation ", i)
		if err := importSimulation(hoverfly, tempDir, paths[i]); err != nil {
			return err
		}

	}

	return nil
}

func importSimulation(hoverfly *hv.Hoverfly, tempDir string, path tmpFile) error {
	data, err := SimFS.ReadFile(path.path)
	if err != nil {
		return fmt.Errorf("unable to read file: %w", err)
	}

	tempFilePath := fmt.Sprintf("%s/%s", tempDir, path.name)
	if err = os.WriteFile(tempFilePath, data, 0o600); err != nil {
		return fmt.Errorf("unable to write file: %w", err)
	}

	if err := hoverfly.ImportFromDisk(tempFilePath); err != nil {
		fmt.Println("unable to import simulation file")
	}
	fmt.Println("Simulation imported: ", path)

	return nil
}
