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
	"time"
	rl "web-crawler/utils"

	_ "github.com/lib/pq"
	"github.com/temoto/robotstxt"
	"golang.org/x/net/html"
)

type page struct {
	url     string
	content string
}

func main() {
	const AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	fmt.Println(AGENT)

	RateLimiter := rl.InitRL()

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

	queue := make(chan string, 2000)
	discovery := make(chan string, 2000)
	entries := make(chan page, 2000)
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

	go func(en chan page, db *sql.DB) {
		statement := `
			INSERT INTO pages (url, content)
			VALUES ($1, $2)
			ON CONFLICT (url) DO UPDATE SET content = EXCLUDED.content, crawled_at = NOW()`
		for e := range en {
			_, err := db.Exec(statement, e.url, e.content)
			if err != nil {
				log.Println("Error adding url:", e.url, "into postgres:", err)
			}
		}
	}(entries, db)
	client := &http.Client{}
	for i := range *workers {
		wg.Add(1)
		go func(ctx context.Context, client *http.Client, id int, q chan string, d chan string, e chan page, db *sql.DB) {
			defer wg.Done()
			for job := range q {
				err = Scrape(ctx, client, RateLimiter, id, job, AGENT, d, e, db, *verbose)
				if err != nil {
					log.Println(err)
				}
			}
		}(ctx, client, i+1, queue, discovery, entries, db)
	}
	wg.Wait()

	fmt.Println("Crawling finished")
}

func Scrape(ctx context.Context, client *http.Client, rl *rl.RateLimiter, id int, Url string, agent string, d chan<- string, e chan page, db *sql.DB, v bool) error {
	if v {
		fmt.Println("Waiting on robots.txt.. Worker", id)
	}
	err := robots(ctx, client, Url, agent)
	if err != nil {
		return err
	}
	if v {
		fmt.Println("Robots allowed... Worker", id)
	}
	base, err := url.Parse(Url)
	if err != nil {
		return err
	}
	host := base.Host
	childCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequest(http.MethodGet, Url, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(childCtx)
	req.Header.Set("User-agent", agent)
	err = rl.Acquire(ctx, host)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
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
							return err
						}
						u := base.ResolveReference(r)
						fmt.Println("Link:", u)
						check := `SELECT EXISTS (SELECT 1 from pages WHERE url=$1)`
						var exists bool
						err = db.QueryRow(check, u.String()).Scan(&exists)
						if err != nil {
							log.Println("error checking link existence, -link:", u)
							return err
						}
						if exists {
							log.Println("url exists, -link:", u, "going ahead")
							continue
						}
						select {
						case <-ctx.Done():
							return nil
						case d <- u.String():
							if v {
								fmt.Println("Sent successfully: -url:", u.String())
							}
							log.Println("Sent successfully: -url:", u.String())
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
			if wordsSoFar > 800 {
				token_state.texton = false
			}
		}
	}
}

func robots(ctx context.Context, client *http.Client, Url string, agent string) error {
	parsedURL, err := url.Parse(Url)
	if err != nil {
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
		return err
	}
	req = req.WithContext(childCtx)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := robotstxt.FromResponse(resp)
	if err != nil {
		return err
	}
	if !data.TestAgent(rq, agent) {
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
