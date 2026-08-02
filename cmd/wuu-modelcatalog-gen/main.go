package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/modelcatalog"
)

func main() {
	input := flag.String("input", modelcatalog.ModelsDevURL, "models.dev api.json URL or local path")
	output := flag.String("output", "internal/modelcatalog/models_dev_catalog.json", "output catalog JSON path")
	flag.Parse()

	data, err := readInput(*input)
	if err != nil {
		fatal(err)
	}
	encoded, _, err := modelcatalog.NormalizeModelsDev(data)
	if err != nil {
		fatal(fmt.Errorf("normalize %s: %w", *input, err))
	}
	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func readInput(input string) ([]byte, error) {
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		client := http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(input)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch %s: %s", input, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(input)
}
