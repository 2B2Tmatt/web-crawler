package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
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
	ctx := context.Background()
	scanner := bufio.NewScanner(os.Stdin)
 
	for{
		fmt.Println()
		fmt.Println("========================================")
		fmt.Println("        Web Crawler Search CLI")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("  q               Quit")
		fmt.Println("  <word> <n>      Search pages by word")
		fmt.Println("                  <word> = term to search")
		fmt.Println("                  <n> (optional) = number of results, default 5")
		fmt.Println()
		fmt.Println("  Example:")
		fmt.Println("      orange 8")
		fmt.Println("      apple")
		fmt.Println()
		fmt.Print("Enter command: ")
		if !scanner.Scan(){
			fmt.Println("Error reading input, exiting.") 
			return
		}
		line := scanner.Text() 
		fields := strings.Fields(line) 
		if len(fields) == 0{
			continue
		}
		if fields[0] == "q"{
			return
		}
		word := fields[0]
		limit := 5 
		if len(fields) >= 2{
			n, err := strconv.Atoi(fields[1])
			if err != nil{
				fmt.Println("Invalid limit value") 
				continue
			}
			limit = n
		}
		fmt.Println("word:", word, "limit:", limit)
		fmt.Println()
		fmt.Println()
		fmt.Println()
		results, err := Search(ctx, db, word, limit)
		if err != nil{
			fmt.Println("Error fetching results: ", err) 
		}
		fmt.Println("")
		fmt.Println("Query Results: ") 
		for i, result := range results{
			fmt.Printf("%d. URL: %s     SCORE:%f\n", i+1, result.url, result.score)
			fmt.Println("CONTENT: \n", result.content) 
			fmt.Println()
			fmt.Println()
		}
		fmt.Println()
		fmt.Println()
		fmt.Println()
		fmt.Println()
	}

	fmt.Println()
	fmt.Println("Exiting...") 
}

type searchResult struct{
	url	string 
	score float64
	content  string 
}

func Search(ctx context.Context, db *sql.DB, word string, limit int) ([]searchResult, error){
	q := `
	WITH q AS (
  	SELECT plainto_tsquery('english', $1) AS query)
	SELECT
  	url,
	content, 
  	ts_rank(content_tsv, q.query) AS score
	FROM search_docs, q
	WHERE content_tsv @@ q.query
	ORDER BY score DESC
	LIMIT $2;
`	
	results := make([]searchResult,0)
	rows, err := db.QueryContext(ctx, q, word, limit)
	if err != nil{
		return nil, err 
	}
	defer rows.Close()
	for rows.Next(){
		var url, content string 
		var score float64
		err = rows.Scan(&url, &content, &score)
		if err != nil{
			return nil, err 
		}
		results = append(results, searchResult{url, score, content})
	}
	return results, nil
}