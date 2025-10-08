package httpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"collab-agent/models"
	"collab-agent/mongodb"
)

var baseDomain = os.Getenv("BASE_DOMAIN")

const fallbackMessage = "<p>This is default page</p>"

type Server struct {
	mongoClient *mongo.Client
}

func NewServer(client *mongo.Client) *Server {
	return &Server{
		mongoClient: client,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.Host, "."+baseDomain) {
		http.Error(w, "Invalid host", http.StatusBadRequest)
		return
	}

	coll := s.mongoClient.Database("collab2").Collection("responses")

	subdomain := strings.TrimSuffix(r.Host, "."+baseDomain)
	parts := strings.Split(subdomain, ".")
	host := parts[len(parts)-1] + "." + baseDomain
	filter := bson.D{{Key: "host", Value: host}}

	cursor, err := coll.Find(context.TODO(), filter)
	if err != nil {
		mongodb.HandleMongoError(err, s.mongoClient)
		http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
		return
	}

	response := []byte(fallbackMessage)
	responseCode := 200
	responseHeaders := []map[string]string{{"key": "Content-Type", "value": "text/html"}}
	currentPriority := -1
	patternId := ""
	externalHandler := ""

	for cursor.Next(context.TODO()) {
		var pattern models.PatternStruct
		if err := cursor.Decode(&pattern); err != nil {
			mongodb.HandleMongoError(err, s.mongoClient)
			http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
			return
		}

		match, err := regexp.MatchString(pattern.Pattern, r.URL.Path)
		if err != nil {
			http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
			return
		}
		if match {
			if currentPriority < pattern.Priority {
				response, err = base64.StdEncoding.DecodeString(pattern.ResponseBody)
				if err != nil {
					http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
					return
				}
				responseCode = pattern.ResponseCode
				responseHeaders = []map[string]string{}
				for _, v := range pattern.ResponseHeaders {
					header := map[string]string{"key": v["key"], "value": v["value"]}
					responseHeaders = append(responseHeaders, header)
				}
				currentPriority = pattern.Priority
				patternId = pattern.Id
				externalHandler = pattern.ExternalHandler
			}
		}
	}

	defer cursor.Close(context.TODO())

	dump, err := httputil.DumpRequest(r, true)
	if err != nil {
		http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
		return
	}

	collReq := s.mongoClient.Database("collab2").Collection("requests")

	var req models.Request
	req.Host = host
	req.Method = r.Method
	req.Path = r.URL.Path
	req.RawRequest = dump
	req.UserIp = r.RemoteAddr
	req.Timestamp = time.Now()
	req.PatternId = patternId

	_, err = collReq.InsertOne(context.TODO(), req)
	if err != nil {
		mongodb.HandleMongoError(err, s.mongoClient)
		http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
		return
	}

	if externalHandler != "" {
		url, err := url.Parse(externalHandler)
		if err != nil {
			http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
			return
		}

		proxy := httputil.NewSingleHostReverseProxy(url)
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, fmt.Sprint(err), http.StatusBadGateway)
		}

		proxy.ServeHTTP(w, r)
		return
	}

	for _, header := range responseHeaders {
		w.Header().Set(string(header["key"]), string(header["value"]))
	}

	w.WriteHeader(responseCode)
	w.Write(response)
}
