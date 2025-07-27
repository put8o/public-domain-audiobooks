package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type ResponseBody struct {
	Results string `json:"results"`
}

type PageWorkload struct {
	Request *http.Request
	Errors  []error
}

func main() {
	retryCount := 10
	pagesToFetch := 824
	workers := runtime.NumCPU() * 4

	requests := make(chan PageWorkload, pagesToFetch)
	responses := make(chan string, pagesToFetch)
	failures := make(chan PageWorkload, pagesToFetch)

	client := &http.Client{Timeout: 20 * time.Second}
	var wg sync.WaitGroup

	wg.Add(pagesToFetch)

	go RequestGenerator(requests, pagesToFetch)

	go func() {
		defer close(requests)
		defer close(failures)
		defer close(responses)

		for range workers {
			go RequestProcessor(requests, responses, failures, client, &wg, retryCount)
		}
		wg.Wait()
	}()

	bookUrls := []string{}

	p := 0
	for r := range responses {
		p++
		fmt.Print("\033[H\033[2J")
		fmt.Printf("processed %v/%v pages\n", p, pagesToFetch)
		bookUrls = append(bookUrls, HTMLProcessor(r)...)
		fmt.Printf("collected %v book URLs\n", len(bookUrls))
	}

	fmt.Println("writing to disk")

	content := strings.Join(bookUrls, "\n")

	err := os.WriteFile("output.txt", []byte(content), 0644)
	if err != nil {
		log.Fatal("failed to write to file:", err)
	}

	errorCounts := make(map[string]int)
	for f := range failures {
		for _, e := range f.Errors {
			errorCounts[e.Error()] = errorCounts[e.Error()] + 1
		}
	}

	if len(errorCounts) > 0 {
		uniqueErrorMessages := []string{}
		fmt.Println("Error messages:")
		for e := range errorCounts {
			uniqueErrorMessages = append(uniqueErrorMessages, fmt.Sprintf("%s: %v", e, errorCounts[e]))
		}

		fmt.Println("writing errors to disk")

		errorContent := strings.Join(uniqueErrorMessages, "\n")

		err = os.WriteFile("errors.txt", []byte(errorContent), 0644)
		if err != nil {
			log.Fatal("failed to write to file:", err)
		}
	}

	fmt.Println("done")

}

func BuildHTTPRequest(pageNumber int) *http.Request {

	url := fmt.Sprintf("https://librivox.org/search/get_results?search_category=title&search_page=%v", pageNumber)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("X-Requested-With", "XMLHttpRequest")

	// fmt.Println("request built")

	return req
}

func RequestGenerator(requests chan PageWorkload, pageCount int) {
	//defer close(requests)
	for i := 1; i <= pageCount; i++ {
		requests <- PageWorkload{Request: BuildHTTPRequest(i)}
	}
}

func ExecuteHTTPRequest(r *http.Request, c *http.Client) (string, error) {

	res, err := c.Do(r)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %v: %s", res.StatusCode, res.Status)
	}

	var rp ResponseBody

	err = json.NewDecoder(res.Body).Decode(&rp)
	if err != nil {
		return "", err
	}

	return rp.Results, nil
}

func RequestProcessor(requests chan PageWorkload, responses chan<- string, failures chan<- PageWorkload, c *http.Client, wg *sync.WaitGroup, retryCount int) {
	for r := range requests {
		// fmt.Println("processing")
		res, err := ExecuteHTTPRequest(r.Request, c)
		if err != nil {
			r.Errors = append(r.Errors, err)
			if len(r.Errors) < retryCount {
				requests <- r
			} else {
				failures <- r
				wg.Done()
			}
		} else {
			responses <- res
			wg.Done()
		}
	}
}

func HTMLProcessor(pageContent string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(pageContent))
	if err != nil {
		log.Fatalf("failed to parse HTML: %v", err)
	}

	var bookUrls = []string{}

	doc.Find("li.catalog-result").Each(func(i int, s *goquery.Selection) {

		c := s.Find("a.book-cover")
		b, exists := c.Attr("href")
		if exists {
			bookUrls = append(bookUrls, b)
		}
	})

	return bookUrls
}
