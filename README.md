# Web Crawler + Text Search CLI

A concurrent web crawler and text-search command line interface inspired by:  
https://www.youtube.com/watch?v=6CJiM3E2mAA&t=115s

This project contains two components:

1. Web Crawler – Recursively crawls pages starting from a seed URL, extracts text, and stores results in Postgres.
2. Text Search CLI – A terminal interface for running keyword searches against the stored text.

---

## CLI Tool Output

<img width="632" height="516" alt="CLI Screenshot" src="https://github.com/user-attachments/assets/1209ef30-b3ec-4c69-92a7-19d1ae024e64" />

## Crawler Output

<img width="736" height="486" alt="Crawler Output" src="https://github.com/user-attachments/assets/2d09946a-2a35-4253-ae85-21fa2b2a29df" />

---

## Requirements

- Go 1.22+
- Docker installed, or a local Postgres instance running

---

## Getting Started

### 1. Clone the Repository

    git clone https://github.com/2B2Tmatt/web-crawler
    cd web-crawler

### 2. Start Postgres

#### Option A – Using Docker (recommended)

    docker compose up -d

#### Option B – Local Postgres

Edit the connection string in `main.go` if needed:

    dbUrl := "postgres://postgres:postgres@localhost:5431/pages?sslmode=disable"

---

## Running the Web Crawler

From the project root:

    go run main.go https://example.com

Default behavior:
- 20 worker goroutines  
- 20-second crawl duration  
- Logs written to db.log  
- Verbose mode disabled  

---

## Optional Flags

| Flag | Type | Description |
|------|------|-------------|
| -w   | int    | Number of worker goroutines (default: 20) |
| -d   | int    | Duration in seconds before cancellation (default: 20) |
| -v   | bool   | Enable verbose stdout output |
| -o   | string | Output log filename (default: db.log) |

### Example

    go run main.go -v -d=120 -w=40 -o=debug.log example.com

---

## Running the Text Search CLI

Navigate to the CLI directory:

    cd cli
    go run cli.go

Usage inside the CLI:

    <search term> <optional limit>

Examples:

    apple
    orange 10

If no limit is provided, the default is 5.

---

