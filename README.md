Web Crawler + Text Search CLI<br>
Inspired by: https://www.youtube.com/watch?v=6CJiM3E2mAA&t=115s<br>
<br>
CLI Tool for Text Search:<br>
<img width="632" height="516" alt="image" src="https://github.com/user-attachments/assets/1209ef30-b3ec-4c69-92a7-19d1ae024e64" />

Standard Output of the Crawler:<br>
<img width="736" height="486" alt="image" src="https://github.com/user-attachments/assets/2d09946a-2a35-4253-ae85-21fa2b2a29df" />
<br><br>

Usage:<br> 

Requirements:<br> 
<li>Go 1.22+</li>
<li>Docker installed or Postgres running locally</li>

<br>
Instructions:
<ol>
  <li>Clone the repository: "git clone https://github.com/2B2Tmatt/web-crawler"</li>
  <li>Run: "docker compose up -d" or set up postgres locally and edit the Database Url in main.go. This program requires a database connection</li>
  <li>In the project root run: "go run main.go https://example.com"(starting url seed) This is to start the web scraping process. The default scrape is a duration of 20 seconds. The argument can be any url, there are some other optional flags, more about them in the next section</li>
  <li>In /cli: "go run cli.go" This is to start the CLI tool.</li>
</ol>

Optional Flags:
<li>-w flag(integer): Determines the number of goroutines spawned in to scrape concurrently. The default is 20 workers. </li>
<li>-d flag(integer): Determines the number of seconds the crawler operates before a cancellation signal is sent. The default is 20 seconds.</li>
<li>-v flag(boolean): Stands for verbose, prints more to standard out. The default is off</li>
<li>-o flag(string):  Stands for output. Logs and errors for debugging will be directed to this file. The default is db.log which will be created on program start.</li>
<br>
Example: "go run main.go -v -d=120 -w=40 -o=debug.log example.com"<br> 
This would start scraping sites starting at example.com, for a duration of 120 seconds, with 40 goroutine workers, and have logs directed into debug.log, with verbose output to stdout. 
