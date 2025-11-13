package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/emersion/go-smtp"
	"github.com/miekg/dns"

	"collab-agent/dnsserver"
	"collab-agent/httpserver"
	"collab-agent/mongodb"
	"collab-agent/smtpserver"
)

func getEnv(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}

var baseDomain = getEnv("BASE_DOMAIN", "localhost")

var mongo_uri = getEnv("MONGO_URI", "mongodb://db:27017/")

var certFile = getEnv("CERT_FILE", "/root/cert.pem")
var keyFile = getEnv("KEY_FILE", "/root/privkey.pem")

var staticRecordsFile = getEnv("STATIC_RECORDS_FILE", "/root/static-records.json")

var staticRecords map[string][]string

func parseStaticRecordsFile() {
	if _, err := os.Stat(staticRecordsFile); os.IsNotExist(err) {
		fmt.Printf("static records file does not exist: %s\n", staticRecordsFile)
		return
	}
	jsonFile, err := os.Open(staticRecordsFile)
	if err != nil {
		fmt.Printf("error opening static records file: %s\n", err)
		os.Exit(1)
	}

	defer jsonFile.Close()

	err = json.NewDecoder(jsonFile).Decode(&staticRecords)
	if err != nil {
		fmt.Printf("error decoding static records file: %s\n", err)
		os.Exit(1)
	}
}

func getClient() *mongo.Client {
	opts := options.Client().ApplyURI(mongo_uri)
	client, err := mongo.Connect(context.TODO(), opts)
	if err != nil {
		fmt.Printf("Failed to connect to MongoDB: %v\n", err)
		os.Exit(1)
	}
	return client
}

func main() {
	mongoClient := getClient()
	defer func() {
		if err := mongoClient.Disconnect(context.TODO()); err != nil {
			mongodb.HandleMongoError(err, mongoClient)
		}
	}()

	httpServer := httpserver.NewServer(mongoClient)
	muxHttp := http.NewServeMux()
	muxHttp.Handle("/", httpServer)

	muxHttps := http.NewServeMux()
	muxHttps.Handle("/", httpServer)

	parseStaticRecordsFile()
	dnsServerUdp := &dns.Server{
		Addr:    ":53",
		Net:     "udp",
		Handler: dnsserver.NewServer(mongoClient, &staticRecords),
	}

	dnsServerTcp := &dns.Server{
		Addr:    ":53",
		Net:     "tcp",
		Handler: dnsserver.NewServer(mongoClient, &staticRecords),
	}

	smtpServer := smtpserver.NewServer(mongoClient)
	smtpServer.Addr = ":25"
	smtpServer.Domain = baseDomain

	smtpsServer := smtpserver.NewServer(mongoClient)
	smtpsServer.Addr = ":587"
	smtpsServer.Domain = baseDomain
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		fmt.Printf("error loading TLS certificate: %s\n", err)
		os.Exit(1)
	}
	smtpsServer.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	go func() {
		err := smtpServer.ListenAndServe()
		if errors.Is(err, smtp.ErrServerClosed) {
			fmt.Printf("SMTP server closed\n")
		} else if err != nil {
			fmt.Printf("error starting SMTP server: %s\n", err)
			os.Exit(1)
		}
	}()

	go func() {
		err := smtpsServer.ListenAndServeTLS()
		if errors.Is(err, smtp.ErrServerClosed) {
			fmt.Printf("SMTPS server closed\n")
		} else if err != nil {
			fmt.Printf("error starting SMTPS server: %s\n", err)
			os.Exit(1)
		}
	}()

	go func() {
		err := dnsServerUdp.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("DNS server closed\n")
		} else if err != nil {
			fmt.Printf("error starting DNS server: %s\n", err)
			os.Exit(1)
		}
	}()

	go func() {
		err := dnsServerTcp.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("DNS server closed\n")
		} else if err != nil {
			fmt.Printf("error starting DNS server: %s\n", err)
			os.Exit(1)
		}
	}()

	go func() {
		err := http.ListenAndServe(":80", muxHttp)
		if errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("HTTP server closed\n")
		} else if err != nil {
			fmt.Printf("error starting HTTP server: %s\n", err)
			os.Exit(1)
		}
	}()

	go func() {
		err := http.ListenAndServeTLS(":443", certFile, keyFile, muxHttps)
		if errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("HTTPS server closed\n")
		} else if err != nil {
			fmt.Printf("error starting HTTPS server: %s\n", err)
			os.Exit(1)
		}
	}()

	// Setup signal handling for SIGHUP
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	go func() {
		for {
			sig := <-sigChan
			if sig == syscall.SIGHUP {
				fmt.Printf("Received SIGHUP, reloading static records...\n")
				parseStaticRecordsFile()
				fmt.Printf("Static records reloaded successfully\n")
			}
		}
	}()

	select {}
}
