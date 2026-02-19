package main

import (
	"crypto/md5"
	"errors"
	"file-transfer/messages"
	"file-transfer/util"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

func handleStorage(msgHandler *messages.MessageHandler, request *messages.StorageRequest) error {
	fileName := request.FileName
	splitList := strings.Split(fileName, "/")
	fileName = splitList[len(splitList)-1]

	log.Printf("Storing %q (%d bytes)", fileName, request.Size)

	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		msgHandler.SendResponse(false, err.Error())
		return fmt.Errorf("open %q: %w", fileName, err)
	}
	defer file.Close()

	msgHandler.SendResponse(true, "Ready for data")

	h := md5.New()
	if _, err := io.CopyN(io.MultiWriter(file, h), msgHandler, int64(request.Size)); err != nil {
		os.Remove(fileName)
		return fmt.Errorf("receiving data: %w", err)
	}

	clientCheckMsg, err := msgHandler.Receive()
	if err != nil {
		os.Remove(fileName)
		return fmt.Errorf("receiving checksum: %w", err)
	}

	clientCheck := clientCheckMsg.GetChecksum().Checksum
	if !util.VerifyChecksum(h.Sum(nil), clientCheck) {
		os.Remove(fileName)
		msgHandler.SendResponse(false, "Checksum mismatch")
		return errors.New("checksum mismatch — file removed")
	}

	msgHandler.SendResponse(true, "File stored successfully")
	log.Printf("Stored %q successfully", fileName)
	return nil
}

func handleRetrieval(msgHandler *messages.MessageHandler, request *messages.RetrievalRequest) error {
	log.Printf("Retrieving %q", request.FileName)

	info, err := os.Stat(request.FileName)
	if err != nil {
		msgHandler.SendRetrievalResponse(false, err.Error(), 0)
		return fmt.Errorf("stat %q: %w", request.FileName, err)
	}

	file, err := os.Open(request.FileName)
	if err != nil {
		msgHandler.SendRetrievalResponse(false, err.Error(), 0)
		return fmt.Errorf("open %q: %w", request.FileName, err)
	}
	defer file.Close()

	msgHandler.SendRetrievalResponse(true, "Ready to send", uint64(info.Size()))

	h := md5.New()
	if _, err := io.Copy(io.MultiWriter(msgHandler, h), file); err != nil {
		return fmt.Errorf("sending data: %w", err)
	}

	msgHandler.SendChecksumVerification(h.Sum(nil))
	log.Printf("Sent %q successfully", request.FileName)
	return nil
}

func handleClient(msgHandler *messages.MessageHandler) {
	defer msgHandler.Close()
	log.Println("Handling new client connection")

	for {
		wrapper, err := msgHandler.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Println("Client disconnected")
			} else {
				log.Println("Receive error:", err)
			}
			return
		}

		switch msg := wrapper.Msg.(type) {
		case *messages.Wrapper_StorageReq:
			if err := handleStorage(msgHandler, msg.StorageReq); err != nil {
				log.Printf("Storage error: %v", err)
			}
		case *messages.Wrapper_RetrievalReq:
			if err := handleRetrieval(msgHandler, msg.RetrievalReq); err != nil {
				log.Printf("Retrieval error: %v", err)
			}
		case nil:
			log.Println("Empty message — closing connection")
			return
		default:
			log.Printf("Unexpected message type: %T", msg)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s port [storage-dir]\n", os.Args[0])
		os.Exit(1)
	}

	port := os.Args[1]

	dir := "."
	if len(os.Args) >= 3 {
		dir = os.Args[2]
	}
	if err := os.Chdir(dir); err != nil {
		log.Fatalf("Cannot chdir to %q: %v\n", dir, err)
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Listen on port %s failed: %v\n", port, err)
	}
	defer listener.Close()

	log.Printf("Listening on port %s, storing files in %q\n", port, dir)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				log.Println("Listener closed, shutting down")
				return
			}
			log.Println("Accept error:", err)
			continue
		}
		log.Println("Accepted connection from", conn.RemoteAddr())
		go handleClient(messages.NewMessageHandler(conn))
	}
}
