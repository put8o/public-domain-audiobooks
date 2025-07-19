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

func main() {
	pagesToFetch := 824
	workers := runtime.NumCPU()

	requests := make(chan *http.Request, pagesToFetch)
	responses := make(chan string, pagesToFetch)

	client := &http.Client{Timeout: 20 * time.Second}
	var wg sync.WaitGroup

	go RequestGenerator(requests, pagesToFetch)

	go func() {
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go RequestProcessor(requests, responses, client, &wg)
		}
		wg.Wait()
		close(responses)
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

func RequestGenerator(requests chan *http.Request, pageCount int) {
	defer close(requests)
	for i := 1; i <= pageCount; i++ {
		requests <- BuildHTTPRequest(i)
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

func RequestProcessor(requests chan *http.Request, responses chan string, c *http.Client, wg *sync.WaitGroup) {
	defer wg.Done()
	for r := range requests {
		// fmt.Println("processing")
		res, err := ExecuteHTTPRequest(r, c)
		if err == nil {
			responses <- res
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
