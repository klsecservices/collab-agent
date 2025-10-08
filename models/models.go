package models

import "time"

type PatternStruct struct {
	Host            string
	Id              string
	ExternalHandler string
	Pattern         string
	Priority        int
	ResponseBody    string
	ResponseCode    int
	ResponseHeaders []map[string]string
}

type Request struct {
	Host       string ``
	PatternId  string
	Method     string
	Path       string
	RawRequest []byte
	UserIp     string
	Timestamp  time.Time
}

type DNSRequest struct {
	Host      string
	QName     string
	QType     string
	UserIp    string
	Timestamp time.Time
}

type SMTPRequest struct {
	From      string
	To        string
	Data      []byte
	UserIp    string
	Host      string
	Timestamp time.Time
}

type DNSRecord struct {
	Host         string
	ResponseType string
	Id           string
	Name         string
	Value        string
	Value1       string
	Value2       string
	TTL          string
	Type         string
}
