package smtpserver

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"collab-agent/models"
	"collab-agent/mongodb"

	"github.com/emersion/go-smtp"
	"go.mongodb.org/mongo-driver/mongo"
)

var baseDomain = os.Getenv("BASE_DOMAIN")

type Server struct {
	mongoClient *mongo.Client
}

func (s *Server) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &Session{mongoClient: s.mongoClient, remoteAddr: c.Conn().RemoteAddr().String()}, nil
}

type Session struct {
	from        string
	to          string
	data        []byte
	mongoClient *mongo.Client
	remoteAddr  string
}

func (s *Session) Reset() {}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.to = to
	return nil
}

func (s *Session) Data(r io.Reader) error {
	if b, err := io.ReadAll(r); err != nil {
		return err
	} else {
		s.data = b
	}

	if strings.Contains(s.to, "@") {
		parts := strings.Split(s.to, "@")
		domain := parts[1]
		if strings.HasSuffix(domain, "."+baseDomain) {
			var req models.SMTPRequest
			req.From = s.from
			req.To = s.to
			req.Data = s.data
			req.UserIp = s.remoteAddr
			req.Host = domain
			req.Timestamp = time.Now()

			coll := s.mongoClient.Database("collab2").Collection("smtp_requests")
			_, err := coll.InsertOne(context.TODO(), req)
			if err != nil {
				mongodb.HandleMongoError(err, s.mongoClient)
			}
		}
	}

	return nil
}

func (s *Session) Logout() error {
	return nil
}

func NewServer(client *mongo.Client) *smtp.Server {
	srv := &Server{mongoClient: client}
	s := smtp.NewServer(srv)

	s.AllowInsecureAuth = true
	s.WriteTimeout = 10 * time.Second
	s.ReadTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024 * 10
	s.MaxRecipients = 100

	return s
}
