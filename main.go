package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/quic-go/quic-go"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("用法: data_cli <ip:port> <ls|get filename>")
		os.Exit(1)
	}

	server := os.Args[1]
	cmd := strings.Join(os.Args[2:], " ")

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
		io.Copy(out, stream)
		fmt.Println("檔案下載完成:", filename)
	} else if cmd == "ls" {
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}
}
