package main

import (
	"crypto/md5"
	"file-transfer/messages"
	"file-transfer/util"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

func put(msgHandler *messages.MessageHandler, fileName string) error {
	fmt.Println("PUT", fileName)

	info, err := os.Stat(fileName)
	if err != nil {
		return fmt.Errorf("stat failed: %w", err)
	}

	msgHandler.SendStorageRequest(fileName, uint64(info.Size()))
	if ok, _ := msgHandler.ReceiveResponse(); !ok {
		return fmt.Errorf("server rejected storage request")
	}

	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	h := md5.New()
	// Single-pass: hash + send simultaneously using buffered pipeline
	if _, err := io.Copy(io.MultiWriter(msgHandler, h), file); err != nil {
		return fmt.Errorf("transfer failed: %w", err)
	}

	msgHandler.SendChecksumVerification(h.Sum(nil))
	if ok, _ := msgHandler.ReceiveResponse(); !ok {
		return fmt.Errorf("checksum verification failed")
	}

	fmt.Println("Storage complete!")
	return nil
}

func get(msgHandler *messages.MessageHandler, fileName string) error {
	fmt.Println("GET", fileName)

	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	msgHandler.SendRetrievalRequest(fileName)
	ok, _, size := msgHandler.ReceiveRetrievalResponse()
	if !ok {
		return fmt.Errorf("server rejected retrieval request")
	}

	h := md5.New()
	// Write to disk and hash in one pass; avoids re-reading file for checksum
	if _, err := io.CopyN(io.MultiWriter(file, h), msgHandler, int64(size)); err != nil {
		return fmt.Errorf("transfer failed: %w", err)
	}

	checkMsg, _ := msgHandler.Receive()
	serverCheck := checkMsg.GetChecksum().Checksum

	if util.VerifyChecksum(serverCheck, h.Sum(nil)) {
		log.Println("Successfully retrieved file.")
	} else {
		// Remove corrupt file to avoid leaving garbage on disk
		os.Remove(fileName)
		return fmt.Errorf("checksum mismatch — corrupt transfer, file removed")
	}

	return nil
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s server:port put|get file-name [download-dir]\n", os.Args[0])
		os.Exit(1)
	}

	host := os.Args[1]
	action := strings.ToLower(os.Args[2])
	fileName := os.Args[3]

	if action != "put" && action != "get" {
		log.Fatalf("Invalid action %q — must be 'put' or 'get'\n", action)
	}

	// Validate optional download directory upfront
	if len(os.Args) >= 5 {
		dir := os.Args[4]
		if _, err := os.Stat(dir); err != nil {
			log.Fatalf("Invalid download directory %q: %v\n", dir, err)
		}
		// Change into the directory so relative paths resolve correctly
		if err := os.Chdir(dir); err != nil {
			log.Fatalf("Cannot chdir to %q: %v\n", dir, err)
		}
	}

	conn, err := net.Dial("tcp", host)
	if err != nil {
		log.Fatalf("Connection failed: %v\n", err)
	}
	defer conn.Close()

	msgHandler := messages.NewMessageHandler(conn)

	var opErr error
	switch action {
	case "put":
		opErr = put(msgHandler, fileName)
	case "get":
		opErr = get(msgHandler, fileName)
	}

	if opErr != nil {
		log.Fatalf("Operation failed: %v\n", opErr)
	}
}