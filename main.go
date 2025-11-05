package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/temoto/robotstxt"
	"golang.org/x/net/html"
)

func main() {
	const AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	fmt.Println(AGENT)
	err := Scrape("https://en.wikipedia.org/wiki/George_Washington", AGENT)
	if err != nil {
		log.Println(err)
	}

}

func Scrape(Url string, agent string) error {
	err := robots(Url, agent)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, Url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-agent", agent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	z := html.NewTokenizer(resp.Body)
	buf := &bytes.Buffer{}
	type state struct {
		skip   bool
		depth  int
		texton bool
	}
	var wordsSoFar int
	token_state := state{false, 0, true}
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			fmt.Println("Content: ", buf.String())
			return nil
		case html.StartTagToken:
			tok := z.Token()
			switch tok.Data {
			case "a":
				for _, attr := range tok.Attr {
					if attr.Key == "href" {
						fmt.Println("Link:", attr.Val)
					}
				}
			case "script":
				token_state.skip = true
				token_state.depth++
			case "style":
				token_state.skip = true
				token_state.depth++
			}
		case html.EndTagToken:
			tok := z.Token()
			switch tok.Data {
			case "script":
				token_state.skip = false
				token_state.depth--
			case "style":
				token_state.skip = false
				token_state.depth--
			}
		case html.TextToken:
			if !token_state.texton {
				continue
			}
			if token_state.skip && token_state.depth > 0 {
				continue
			}
			t := strings.TrimSpace(z.Token().Data)
			if len(t) > 0 {
				buf.Write([]byte(t))
				buf.Write([]byte(" "))
			}
			wordsSoFar += len(strings.Fields(t))
			if wordsSoFar > 800 {
				fmt.Println(buf.String())
				token_state.texton = false
			}
		}
	}
}

func robots(Url string, agent string) error {
	parsedURL, err := url.Parse(Url)
	if err != nil {
		return err
	}
	rq := parsedURL.EscapedPath()
	if parsedURL.RawQuery != "" {
		rq += "?" + parsedURL.RawQuery
	}
	resp, err := http.Get(parsedURL.Scheme + "://" + parsedURL.Host + "/robots.txt")
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
