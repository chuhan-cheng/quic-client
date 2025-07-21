package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
)

type ProgressReader struct {
	r            io.Reader
	totalSize    int64
	readBytes    int64
	lastReadTime time.Time
	lastBytes    int64
}

func NewProgressReader(r io.Reader, totalSize int64) *ProgressReader {
	return &ProgressReader{
		r:            r,
		totalSize:    totalSize,
		lastReadTime: time.Now(),
	}
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.readBytes += int64(n)
	return n, err
}

func (pr *ProgressReader) StartMonitor() {
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for range ticker.C {
			now := time.Now()
			duration := now.Sub(pr.lastReadTime).Seconds()
			diff := pr.readBytes - pr.lastBytes

			speed := float64(diff) / duration
			percent := float64(pr.readBytes) / float64(pr.totalSize) * 100

			fmt.Printf("\r%.2f%% - %.2f KB/s", percent, speed/1024)

			pr.lastReadTime = now
			pr.lastBytes = pr.readBytes

			if pr.readBytes >= pr.totalSize {
				ticker.Stop()
				fmt.Print("\r100.00% - completed\n")
				break
			}
		}
	}()
}

type rateLimitedReader struct {
	r         io.Reader
	limit     int // bytes per second
	lastRead  time.Time
	byteCount int
}

func NewRateLimitedReader(r io.Reader, limit int) io.Reader {
	return &rateLimitedReader{r: r, limit: limit}
}

func (rl *rateLimitedReader) Read(p []byte) (int, error) {
	if rl.limit <= 0 {
		return rl.r.Read(p)
	}

	if rl.lastRead.IsZero() {
		rl.lastRead = time.Now()
	}

	// 限制每次讀取不超過 limit / 10 bytes（100ms 配額）
	maxBytes := rl.limit / 10
	if maxBytes < 1 {
		maxBytes = 1
	}
	if len(p) > maxBytes {
		p = p[:maxBytes]
	}

	n, err := rl.r.Read(p)
	timeElapsed := time.Since(rl.lastRead)
	rl.lastRead = time.Now()

	// sleep 根據傳輸速率補償
	expectedTime := time.Duration(n*int(time.Second)) / time.Duration(rl.limit)
	if timeElapsed < expectedTime {
		time.Sleep(expectedTime - timeElapsed)
	}
	return n, err
}

func main() {
	// 加入 --limit 參數（單位：bytes/sec）
	limit := flag.Int("limit", 0, "下載速度上限 (bytes/sec)，預設不限速")

	flag.Parse()
	args := flag.Args()
	if len(args) < 2 {
		fmt.Println("用法: data_cli [--limit bytes/sec] <ip:port> <ls|get filename>")
		os.Exit(1)
	}

	server := args[0]
	cmd := strings.Join(args[1:], " ")

	session, err := quic.DialAddr(context.Background(), server, &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"data-transfer"}}, nil)

	if err != nil {
		log.Fatal(err)
	}
	stream, err := session.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintln(stream, cmd)

	if strings.HasPrefix(cmd, "get ") {
		filename := strings.TrimPrefix(cmd, "get ")
		out, err := os.Create(filename)
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()

		// 讀取檔案大小（server 傳來的第一行）
		sizeReader := bufio.NewReader(stream)
		sizeLine, err := sizeReader.ReadString('\n')
		if err != nil {
			log.Fatalf("無法讀取檔案大小: %v", err)
		}
		var totalSize int64
		fmt.Sscanf(sizeLine, "%d", &totalSize)

		var reader io.Reader = sizeReader // stream 已被 bufio 包住
		if *limit > 0 {
			reader = NewRateLimitedReader(reader, *limit)
		}

		progressReader := NewProgressReader(reader, totalSize)
		progressReader.StartMonitor()

		io.Copy(out, progressReader)
		fmt.Println("檔案下載完成:", filename)
	} else if cmd == "ls" {
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}
}
