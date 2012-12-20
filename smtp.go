package service

import (
    "encoding/base64"
    "fmt"
    "net/mail"
    "net/smtp"
    "sync"
    "time"
)

const (
    //authTypes
    Plain       = "plain"
    MD5         = "cram-md5"
    Unencrypted = "unencrypted"

    //tags
    SMTP = "smtp"
)

type SmtpServer struct {
    lock    sync.Mutex
    rate    time.Time
    client  *smtp.Client
    auth    smtp.Auth
    running bool
    timeout chan time.Duration
    msg     chan *Message
    conf    *SmtpConf
    t       int //TODO delete after test
}

//smtp.PlainAuth refuses to send your password over an unencrypted connection.
//this auth will use plain authentication anyways.
type unencryptedAuth struct {
    smtp.Auth
}

func (a unencryptedAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
    s := *server
    s.TLS = true
    return a.Auth.Start(&s)
}

type SmtpConf struct {
    Host     string
    Port     int
    Username string
    Password string
    AuthType string
}

func (conf *SmtpConf) smtpAuth() (auth smtp.Auth) {
    switch conf.AuthType {
    case Plain:
        auth = smtp.PlainAuth("", conf.Username, conf.Password, conf.Host)
    case MD5:
        auth = smtp.CRAMMD5Auth(conf.Username, conf.Password)
    case Unencrypted:
        auth = unencryptedAuth{
            smtp.PlainAuth("", conf.Username, conf.Password, conf.Host),
        }
    default:
    }
    return
}

func NewSmtpServer(conf *SmtpConf) (server Server) {
    server = &SmtpServer{
        rate:    time.Now(),
        auth:    conf.smtpAuth(),
        running: false,
        timeout: make(chan time.Duration, 1),
        msg:     make(chan *Message, 1),
        conf:    conf,
    }
    return
}

func (s *SmtpServer) Rate() time.Time {
    if len(s.timeout) != 0 {
        <-s.timeout
        return s.rate
    }
    s.rate = s.rate.Add(time.Duration(len(s.msg)*2) * time.Second)
    return s.rate
}

func (s *SmtpServer) Tag() string {
    return SMTP
}

func (s *SmtpServer) Running() bool {
    return s.running
}

func (s *SmtpServer) Send(m *Message) (err error) {
    s.msg <- m
    err = s.work()
    return
}

func (s *SmtpServer) Close(t time.Duration) (err error) {
    s.timeout <- t
    err = s.work()
    return
}

func (s *SmtpServer) work() (err error) {
    select {
    case m := <-s.msg:
        err = s.send(m)
    case t := <-s.timeout:
        err = s.closeConn(t)
    }
    return
}

func (s *SmtpServer) closeConn(t time.Duration) (err error) {
    s.lock.Lock()
    defer s.lock.Unlock()
    if s.running && time.Now().Sub(s.rate) > t {
        err = s.client.Quit()
        s.running = false
        fmt.Printf("server=%d---------close\n", s.t)
    }
    return
}

func (s *SmtpServer) reConn() (err error) {
    s.client, err = smtp.Dial(fmt.Sprintf("%s:%d", s.conf.Host, s.conf.Port))
    if err != nil {
        //TODO
    }
    fmt.Printf("server=%d---------reopen\n", s.t)
    if ok, _ := s.client.Extension("StartTLS"); ok {
        if err = s.client.StartTLS(nil); err != nil {
            fmt.Printf("tls err=%v\n", err)
            panic(err)
        }
    }
    if s.auth != nil {
        err = s.client.Auth(s.auth)
        if err != nil {
            //TODO
            return
        }
    }
    s.running = true
    return
}

func (s *SmtpServer) send(m *Message) (err error) {
    s.lock.Lock()
    defer func() {
        if len(s.timeout) != 0 {
            <-s.timeout
        }
        s.lock.Unlock()
    }()
    s.rate = time.Now()

    if !s.running {
        err = s.reConn()
        if err != nil {
            //TODO
        }
    }

    if m.Mass {
        err = s.massSend(s.client, m, s.getFromAddr(m))
        time.Sleep(1 * time.Second)
        fmt.Printf("server=%d, msg=%v, err=%v\n", s.t, *m, err)
        return
    }
    err = s.singleSend(s.client, m, s.getFromAddr(m))
    time.Sleep(1 * time.Second)
    fmt.Printf("server=%d, msg=%v, err=%v\n", s.t, *m, err)
    return
}

func (s *SmtpServer) getFromAddr(m *Message) string {
    if s.auth == nil {
        return m.From
    }
    return s.conf.Username
}

func (s *SmtpServer) formatBody(m *Message, to string) []byte {
    b64 := base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/")
    header := make(map[string]string, 6)
    from := mail.Address{m.SenderName, s.getFromAddr(m)}
    header["From"] = from.String()
    header["To"] = to
    header["Subject"] = fmt.Sprintf("=?UTF-8?B?%s?=", b64.EncodeToString([]byte(m.Subject)))
    header["MIME-Version"] = "1.0"
    header["Content-Type"] = "text/html; charset=UTF-8"
    header["Content-Transfer-Encoding"] = "base64"

    body := ""
    for k, v := range header {
        body += fmt.Sprintf("%s: %s\r\n", k, v)
    }
    body += "\r\n" + b64.EncodeToString(m.Body)

    return []byte(body)
}

func (s *SmtpServer) massSend(client *smtp.Client, m *Message, sendAddr string) (err error) {
    err = s.client.Mail(sendAddr)
    if err != nil {
        //TODO
        return
    }
    toStr := ""
    for name, addr := range m.To {
        to := mail.Address{name, addr}
        toStr += to.String() + ", "
        if err = client.Rcpt(addr); err != nil {
            //TODO
            return
        }
    }
    w, err := client.Data()
    if err != nil {
        //TODO
        return
    }
    if _, err = w.Write(s.formatBody(m, toStr)); err != nil {
        //TODO
        return
    }
    err = w.Close()
    return
}

func (s *SmtpServer) singleSend(client *smtp.Client, m *Message, sendAddr string) (err error) {
    for name, addr := range m.To {
        err = s.client.Mail(sendAddr)
        if err != nil {
            //TODO
            return
        }
        if err = client.Rcpt(addr); err != nil {
            //TODO
            break
        }
        w, err := client.Data()
        if err != nil {
            //TODO
            break
        }
        to := mail.Address{name, addr}
        toStr := to.String()
        if _, err = w.Write(s.formatBody(m, toStr)); err != nil {
            //TODO
            break
        }
        if err = w.Close(); err != nil {
            break
        }
    }
    return
}
