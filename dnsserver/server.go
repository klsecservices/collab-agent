package dnsserver

import (
	"collab-agent/models"
	"collab-agent/mongodb"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/miekg/dns"
)

var baseDomain = os.Getenv("BASE_DOMAIN")
var collabIP = os.Getenv("COLLAB_IP")

type Server struct {
	mongoClient *mongo.Client
	rebindCache map[string]bool
	cacheMutex  sync.RWMutex
}

func NewServer(client *mongo.Client) *Server {
	return &Server{
		mongoClient: client,
		rebindCache: make(map[string]bool),
	}
}

func (s *Server) SetRebindCache(key string, value bool) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()
	s.rebindCache[key] = value
}

func (s *Server) GetRebindCache(key string) (bool, bool) {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()
	value, exists := s.rebindCache[key]
	return value, exists
}

func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	msg.Authoritative = true
	coll := s.mongoClient.Database("collab2").Collection("dns_requests")
	records := s.mongoClient.Database("collab2").Collection("dns_records")

	for _, question := range r.Question {
		lowerQuestionName := strings.ToLower(question.Name)
		fmt.Println("Got question: ", lowerQuestionName, " type: ", dns.TypeToString[question.Qtype])

		handled := false

		if strings.HasSuffix(lowerQuestionName, "."+baseDomain+".") {

			// get answer
			lowerQuestionNameLocal := strings.TrimSuffix(lowerQuestionName, "."+baseDomain+".")
			lastIndex := strings.LastIndex(lowerQuestionNameLocal, ".")
			host := lowerQuestionNameLocal[lastIndex+1:] + "." + baseDomain
			var name string
			if lastIndex == -1 {
				name = "@"
			} else {
				name = lowerQuestionNameLocal[:lastIndex]
			}
			var cursor *mongo.Cursor
			var err error
			if question.Qtype == dns.TypeANY {
				cursor, err = records.Find(context.TODO(), bson.M{"name": name, "host": host})
				if err != nil {
					mongodb.HandleMongoError(err, s.mongoClient)
				}
			} else {
				cursor, err = records.Find(context.TODO(), bson.M{"name": name, "host": host, "type": dns.TypeToString[question.Qtype]})
				if err != nil {
					mongodb.HandleMongoError(err, s.mongoClient)
				}
			}
			defer cursor.Close(context.TODO())
			for cursor.Next(context.TODO()) {
				handled = true
				var record models.DNSRecord
				err = cursor.Decode(&record)
				if err != nil {
					mongodb.HandleMongoError(err, s.mongoClient)
				}

				var ttlInt uint32
				var value string

				if record.ResponseType == "rebind" {
					ttlInt = uint32(0)
					alreadyReplied, exists := s.GetRebindCache(record.Id)
					if !exists {
						alreadyReplied = false
					}
					if !alreadyReplied {
						value = record.Value1
					} else {
						value = record.Value2
					}
					s.SetRebindCache(record.Id, !alreadyReplied)
				} else if record.ResponseType == "static" {
					ttl, err := strconv.ParseUint(record.TTL, 10, 32)
					if err != nil {
						fmt.Println("Error parsing TTL: ", err)
						continue
					}
					ttlInt = uint32(ttl)
					value = record.Value
				} else {
					fmt.Println("Unknown response type: " + record.ResponseType)
					continue
				}

				if record.Type == "A" {
					msg.Answer = append(msg.Answer, &dns.A{
						Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttlInt},
						A:   net.ParseIP(value),
					})
				} else if record.Type == "AAAA" {
					msg.Answer = append(msg.Answer, &dns.AAAA{
						Hdr:  dns.RR_Header{Name: question.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttlInt},
						AAAA: net.ParseIP(value),
					})
				} else if record.Type == "CNAME" {
					msg.Answer = append(msg.Answer, &dns.CNAME{
						Hdr:    dns.RR_Header{Name: question.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: ttlInt},
						Target: value,
					})
				} else if record.Type == "MX" {
					msg.Answer = append(msg.Answer, &dns.MX{
						Hdr:        dns.RR_Header{Name: question.Name, Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: ttlInt},
						Mx:         value,
						Preference: 10,
					})
				} else if record.Type == "TXT" {
					msg.Answer = append(msg.Answer, &dns.TXT{
						Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: ttlInt},
						Txt: []string{value},
					})
				} else if record.Type == "NS" {
					msg.Answer = append(msg.Answer, &dns.NS{
						Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: ttlInt},
						Ns:  value,
					})
				} else if record.Type == "PTR" {
					msg.Answer = append(msg.Answer, &dns.PTR{
						Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttlInt},
						Ptr: value,
					})
				}
			}

			// save request
			var req models.DNSRequest
			subdomain := strings.TrimSuffix(lowerQuestionName, "."+baseDomain+".")
			parts := strings.Split(subdomain, ".")
			req.Host = parts[len(parts)-1] + "." + baseDomain
			req.QName = question.Name
			req.QType = dns.TypeToString[question.Qtype]
			req.UserIp = w.RemoteAddr().String()
			req.Timestamp = time.Now()

			_, err = coll.InsertOne(context.TODO(), req)
			if err != nil {
				mongodb.HandleMongoError(err, s.mongoClient)
			}
		}

		// default handler
		if !handled && (strings.HasSuffix(lowerQuestionName, "."+baseDomain+".") || lowerQuestionName == baseDomain+".") {
			if question.Qtype == dns.TypeA {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 21600},
					A:   net.ParseIP(collabIP),
				})
			} else if question.Qtype == dns.TypeNS {
				msg.Answer = append(msg.Answer, &dns.NS{
					Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 3600},
					Ns:  "ns1." + baseDomain + ".",
				})
			} else if question.Qtype == dns.TypeMX {
				fmt.Println("MX: " + question.Name)
				msg.Answer = append(msg.Answer, &dns.MX{
					Hdr:        dns.RR_Header{Name: question.Name, Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 604800},
					Mx:         "mail." + baseDomain + ".",
					Preference: 10,
				})
			} else if question.Qtype == dns.TypeSOA {
				msg.Answer = append(msg.Answer, &dns.SOA{
					Hdr:     dns.RR_Header{Name: question.Name, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 604800},
					Ns:      "ns1." + baseDomain + ".",
					Mbox:    "mail." + baseDomain + ".",
					Serial:  1,
					Refresh: 10800,
					Retry:   3600,
					Expire:  604800,
					Minttl:  60,
				})
			}
		}
	}

	w.WriteMsg(&msg)
}
