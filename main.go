package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/emersion/go-smtp"
	"github.com/miekg/dns"

	"collab-agent/dnsserver"
	"collab-agent/httpserver"
	"collab-agent/mongodb"
	"collab-agent/smtpserver"
)

var baseDomain = os.Getenv("BASE_DOMAIN")

const mongo_uri = "mongodb://db:27017/"

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

	dnsServerUdp := &dns.Server{
		Addr:    ":53",
		Net:     "udp",
		Handler: dnsserver.NewServer(mongoClient),
	}

	dnsServerTcp := &dns.Server{
		Addr:    ":53",
		Net:     "tcp",
		Handler: dnsserver.NewServer(mongoClient),
	}

	smtpServer := smtpserver.NewServer(mongoClient)
	smtpServer.Addr = ":25"
	smtpServer.Domain = baseDomain

	smtpsServer := smtpserver.NewServer(mongoClient)
	smtpsServer.Addr = ":587"
	smtpsServer.Domain = baseDomain
	cert, err := tls.LoadX509KeyPair("/root/cert.pem", "/root/privkey.pem")
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
		err := http.ListenAndServeTLS(":443", "/root/cert.pem", "/root/privkey.pem", muxHttps)
		if errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("HTTPS server closed\n")
		} else if err != nil {
			fmt.Printf("error starting HTTPS server: %s\n", err)
			os.Exit(1)
		}
	}()

	select {}
}
