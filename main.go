package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"web-crawler/utils"

	_ "github.com/lib/pq"
	"github.com/temoto/robotstxt"
	"golang.org/x/net/html"
)

func main() {
	const AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	fmt.Println(AGENT)

	stats := initStats()
	rateLimiter := utils.InitRL()
	domainCache, urlStore := utils.InitStore()

	start := time.Now()

	dbUrl := "postgres://postgres:postgres@localhost:5431/pages?sslmode=disable"
	db, err := sql.Open("postgres", dbUrl)
	if err != nil {
		log.Fatalln("Error starting postgres: ", err)
	}
	defer db.Close()
	err = db.Ping()
	if err != nil {
		log.Fatalln("Error pinging pg service: ", err)
	}

	err = initDB(context.Background(), db)
	if err != nil {
		log.Fatalln("Error while init db: ", err)
	}

	queue := make(chan string, 5000)
	discovery := make(chan string, 5000)
	entries := make(chan page, 5000)
	defer close(entries)

	workers := flag.Int("w", 20, "num web crawlers")
	duration := flag.Int("d", 20, "duration of crawling")
	verbose := flag.Bool("v", false, "explicit logs")
	output := flag.String("o", "db.log", "redirect logs")
	flag.Parse()

	logFile, err := os.OpenFile(*output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Println("Error logging contents")
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	fmt.Println(*workers, "workers starting...")
	discovery <- flag.Args()[0]

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*duration)*time.Second)
	defer cancel()

	go func(q chan string, d chan string, ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				close(q)
				return
			case job := <-d:
				q <- job
			}
		}
	}(queue, discovery, ctx)
	for range 10 {
		go func(en chan page, db *sql.DB, s *sessionStatistics) {
			statement := `
			INSERT INTO pages (url, content)
			VALUES ($1, $2)
			ON CONFLICT (url) DO UPDATE SET content = EXCLUDED.content, crawled_at = NOW()`
			for e := range en {
				_, err := db.Exec(statement, e.url, e.content)
				if err != nil {
					s.IncErrorNetwork()
					s.IncErrorTotal()
					log.Println("Error adding url:", e.url, "into postgres:", err)
				}
				if *verbose {
					fmt.Println("db: -url added:", e.url)
				}
				s.IncDiscoveredNew()
				log.Println("db-write: -url added:", e.url)
			}
		}(entries, db, stats)
	}
	client := &http.Client{}
	deps := dep{rateLimiter, urlStore, domainCache, db, *verbose, AGENT, client}

	for i := range *workers {
		wg.Add(1)
		go func(ctx context.Context, id int, q chan string, d chan string, e chan page, deps dep, s *sessionStatistics) {
			defer wg.Done()
			for job := range q {
				err = Scrape(ctx, id, job, d, e, deps, s)
				if err != nil {
					log.Println(err)
				}
			}
		}(ctx, i+1, queue, discovery, entries, deps, stats)
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	fmt.Println("===================================================")
	fmt.Println("                  SESSION SUMMARY                  ")
	fmt.Println("===================================================")
	fmt.Println()

	fmt.Println("TOTAL PAGES CRAWLED      					:", stats.TotalPages())
	fmt.Println("PAGES PER SECOND      					 	:", stats.PagesPerSecond(int(elapsed)))
	fmt.Println()

	fmt.Println("URLS DISCOVERED        						:", stats.UrlsDiscovered())
	fmt.Println("    - new links found 					   	:", stats.DiscoveredNew())
	fmt.Println()

	fmt.Println("URLS SKIPPED             					:", stats.UrlsSkipped())
	fmt.Println("    - already in cache   					:", stats.SkippedCached())
	fmt.Println()

	fmt.Println("ERROR COUNT              					:", stats.ErrorTotal())
	fmt.Println("    - network errors or context timeout    			:", stats.ErrorNetwork())
	fmt.Println("    - parse failures     					:", stats.ErrorParse())
	fmt.Println("    - robots fetch error 					:", stats.ErrorRobots())
	fmt.Println()

	fmt.Println("===================================================")

}

func Scrape(ctx context.Context, id int, Url string,
	d chan<- string, e chan page, deps dep, s *sessionStatistics) error {

	base, err := url.Parse(Url)
	if err != nil {
		s.IncErrorParse()
		s.IncErrorTotal()
		return err
	}
	host := base.Host
	if deps.verbose {
		fmt.Println("Waiting on robots.txt.. Worker", id)
	}
	allowed, exists := deps.domainCache.CheckExisting(host)
	if !exists {
		err = robots(ctx, deps.client, Url, deps.agent, s)
		if err != nil {
			log.Println("cache: -restricted domain:", host)
			deps.domainCache.Add(host, false)
			return err
		}
		deps.domainCache.Add(host, true)
		log.Println("cache: -allowed domain:", host)
	} else {
		if !allowed {
			s.IncErrorTotal()
			s.IncErrorRobots()
			return errors.New("honoring robots.txt")
		}
		log.Println("cache: -used -domain:", host)
	}

	if deps.verbose {
		fmt.Println("Robots allowed... Worker", id)
	}

	childCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequest(http.MethodGet, Url, nil)
	if err != nil {
		s.IncErrorTotal()
		s.IncErrorNetwork()
		return err
	}
	req = req.WithContext(childCtx)
	req.Header.Set("User-agent", deps.agent)
	err = deps.rateLimiter.Acquire(ctx, host)
	if err != nil {
		s.IncErrorTotal()
		s.IncErrorNetwork()
		return err
	}
	resp, err := deps.client.Do(req)
	if err != nil {
		s.IncErrorTotal()
		s.IncErrorNetwork()
		return err
	}
	defer resp.Body.Close()
	z := html.NewTokenizer(resp.Body)
	buf := &bytes.Buffer{}
	type state struct {
		depth  int
		texton bool
	}
	var wordsSoFar int
	s.IncTotalPages()
	token_state := state{0, true}
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			e <- page{url: Url, content: buf.String()}
			return nil
		case html.StartTagToken:
			tok := z.Token()
			switch tok.Data {
			case "a":
				for _, attr := range tok.Attr {
					if attr.Key == "href" {
						rel := attr.Val
						r, err := url.Parse(rel)
						if err != nil {
							s.IncErrorTotal()
							s.IncErrorParse()
							return err
						}
						u := base.ResolveReference(r)
						fmt.Println("Link:", u)
						s.IncUrlsDiscovered()
						prior := deps.urlStore.CheckExisting(u.String())
						if !prior {
							deps.urlStore.Add(u.String())
						} else {
							log.Println("url exists -link:", u, "going to the next token -cache used")
							s.IncUrlsSkipped()
							s.IncSkippedCached()
							continue
						}

						select {
						case <-ctx.Done():
							return nil
						case d <- u.String():
							if deps.verbose {
								fmt.Println("Sent successfully: -url:", u.String())
							}
							log.Println("Sent successfully: -url:", u.String())
						default:
							log.Println("Channel full-moving on")
							continue
						}
					}
				}
			case "script":
				token_state.depth++
			case "style":
				token_state.depth++
			}
		case html.EndTagToken:
			tok := z.Token()
			switch tok.Data {
			case "script":
				token_state.depth--
			case "style":
				token_state.depth--
			}
		case html.TextToken:
			if !token_state.texton {
				continue
			}
			if token_state.depth > 0 {
				continue
			}
			t := strings.TrimSpace(z.Token().Data)
			if len(t) > 0 && !strings.Contains(t, "<iframe") {
				buf.Write([]byte(t))
				buf.Write([]byte(" "))
			}
			wordsSoFar += len(strings.Fields(t))
			if wordsSoFar > 500 {
				token_state.texton = false
			}
		}
	}
}

func robots(ctx context.Context, client *http.Client, Url string, agent string, s *sessionStatistics) error {
	parsedURL, err := url.Parse(Url)
	if err != nil {
		s.IncErrorTotal()
		s.IncErrorParse()
		return err
	}
	rq := parsedURL.EscapedPath()
	if parsedURL.RawQuery != "" {
		rq += "?" + parsedURL.RawQuery
	}
	childCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequest(http.MethodGet, parsedURL.Scheme+"://"+parsedURL.Host+"/robots.txt", nil)
	if err != nil {
		s.IncErrorTotal()
		s.IncErrorNetwork()
		return err
	}
	req = req.WithContext(childCtx)
	resp, err := client.Do(req)
	if err != nil {
		s.IncErrorTotal()
		s.IncErrorNetwork()
		return err
	}
	defer resp.Body.Close()
	data, err := robotstxt.FromResponse(resp)
	if err != nil {
		s.IncErrorTotal()
		s.IncErrorNetwork()
		return err
	}
	if !data.TestAgent(rq, agent) {
		s.IncErrorTotal()
		s.IncErrorRobots()
		return errors.New("honoring robots.txt")
	}
	return nil
}

func initDB(ctx context.Context, db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS pages(
		url TEXT PRIMARY KEY, 
		content TEXT, 
		crawled_at TIMESTAMPTZ DEFAULT NOW()
	);`

	_, err := db.ExecContext(ctx, schema)
	if err != nil {
		return err
	}
	return nil
}

type page struct {
	url     string
	content string
}

type dep struct {
	rateLimiter *utils.RateLimiter
	urlStore    *utils.UrlMemory
	domainCache *utils.DomainCache
	database    *sql.DB
	verbose     bool
	agent       string
	client      *http.Client
}

type sessionStatistics struct {
	urlsDiscovered uint64
	discoveredNew  uint64
	urlsSkipped    uint64
	skippedCached  uint64
	skippedDB      uint64
	skippedRobots  uint64
	errorTotal     uint64
	errorNetwork   uint64
	errorParse     uint64
	errorRobots    uint64
	totalPages     uint64
}

func (ss *sessionStatistics) PagesPerSecond(seconds int) int {
	return int(ss.totalPages) / seconds
}

func (ss *sessionStatistics) UrlsDiscovered() uint64 {
	return atomic.LoadUint64(&ss.urlsDiscovered)
}
func (ss *sessionStatistics) IncUrlsDiscovered() {
	atomic.AddUint64(&ss.urlsDiscovered, 1)
}
func (ss *sessionStatistics) DiscoveredNew() uint64 {
	return atomic.LoadUint64(&ss.discoveredNew)
}
func (ss *sessionStatistics) IncDiscoveredNew() {
	atomic.AddUint64(&ss.discoveredNew, 1)
}

func (ss *sessionStatistics) UrlsSkipped() uint64 {
	return atomic.LoadUint64(&ss.urlsSkipped)
}
func (ss *sessionStatistics) IncUrlsSkipped() {
	atomic.AddUint64(&ss.urlsSkipped, 1)
}
func (ss *sessionStatistics) SkippedCached() uint64 {
	return atomic.LoadUint64(&ss.skippedCached)
}
func (ss *sessionStatistics) IncSkippedCached() {
	atomic.AddUint64(&ss.skippedCached, 1)
}
func (ss *sessionStatistics) SkippedDB() uint64 {
	return atomic.LoadUint64(&ss.skippedDB)
}
func (ss *sessionStatistics) IncSkippedDB() {
	atomic.AddUint64(&ss.skippedDB, 1)
}

func (ss *sessionStatistics) SkippedRobots() uint64 {
	return atomic.LoadUint64(&ss.skippedRobots)
}
func (ss *sessionStatistics) IncSkippedRobots() {
	atomic.AddUint64(&ss.skippedRobots, 1)
}

func (ss *sessionStatistics) ErrorTotal() uint64 {
	return atomic.LoadUint64(&ss.errorTotal)
}
func (ss *sessionStatistics) IncErrorTotal() {
	atomic.AddUint64(&ss.errorTotal, 1)
}
func (ss *sessionStatistics) ErrorNetwork() uint64 {
	return atomic.LoadUint64(&ss.errorNetwork)
}
func (ss *sessionStatistics) IncErrorNetwork() {
	atomic.AddUint64(&ss.errorNetwork, 1)
}

func (ss *sessionStatistics) ErrorParse() uint64 {
	return atomic.LoadUint64(&ss.errorParse)
}
func (ss *sessionStatistics) IncErrorParse() {
	atomic.AddUint64(&ss.errorParse, 1)
}

func (ss *sessionStatistics) ErrorRobots() uint64 {
	return atomic.LoadUint64(&ss.errorRobots)
}
func (ss *sessionStatistics) IncErrorRobots() {
	atomic.AddUint64(&ss.errorRobots, 1)
}

func (ss *sessionStatistics) TotalPages() uint64 {
	return atomic.LoadUint64(&ss.totalPages)
}
func (ss *sessionStatistics) IncTotalPages() {
	atomic.AddUint64(&ss.totalPages, 1)
}

func initStats() *sessionStatistics {
	return &sessionStatistics{}
}
