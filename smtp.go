package service

import (
    "bytes"
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

    //conf
    Limit = 60 * 60
    Step  = 21 * 60
)

type SmtpServer struct {
    lock    sync.Mutex
    rate    time.Time
    client  *smtp.Client
    auth    smtp.Auth
    running bool
    conf    *SmtpConf
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
    //options
    TimeOut   uint32
    SickLimit uint32
    SickStep  uint32
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
        auth: conf.smtpAuth(),
        conf: conf,
    }
    return
}

func (s *SmtpServer) Init(conf ...interface{}) (err error) {
    s.rate = time.Now()
    if s.conf.TimeOut == 0 {
        s.conf.TimeOut = Step / 8
    }
    if s.conf.SickLimit == 0 {
        s.conf.SickLimit = Limit
    }
    if s.conf.SickStep == 0 {
        s.conf.SickStep = Step
    }
    return s.connect()
}

func (s *SmtpServer) Send(m *Message) error {
    return s.send(m)
}

func (s *SmtpServer) Close() (err error) {
    s.lock.Lock()
    defer s.lock.Unlock()
    if s.Timeout() {
        err = s.closeConn()
    }
    return
}

func (s *SmtpServer) Rate() time.Time {
    return s.rate
}

func (s *SmtpServer) Tag() string {
    return SMTP
}

func (s *SmtpServer) Running() bool {
    return s.running
}

func (s *SmtpServer) Timeout() bool {
    return time.Now().Sub(s.rate) > time.Duration(s.conf.TimeOut)*time.Second
}

func (s *SmtpServer) Sick() bool {
    return s.rate.Sub(time.Now()) > time.Duration(s.conf.SickLimit)*time.Second
}

func (s *SmtpServer) closeConn() (err error) {
    if s.running {
        err = s.client.Quit()
        s.running = false
    }
    return
}

func (s *SmtpServer) connect() (err error) {
    s.client, err = smtp.Dial(fmt.Sprintf("%s:%d", s.conf.Host, s.conf.Port))
    if err != nil {
        return
    }
    if s.auth != nil {
        if ok, _ := s.client.Extension("StartTLS"); ok {
            if err = s.client.StartTLS(nil); err != nil {
                return
            }
        }
        err = s.client.Auth(s.auth)
        if err != nil {
            return
        }
    }
    s.running = true
    return
}

func (s *SmtpServer) upgrade() {
    s.rate = s.rate.Add(time.Duration(s.conf.SickStep) * time.Second)
}

func (s *SmtpServer) send(m *Message) (err error) {
    s.lock.Lock()
    defer s.lock.Unlock()

    if !s.running {
        err = s.connect()
        if err != nil {
            s.upgrade()
            return
        }
    }

    if m.Mass {
        err = s.massSend(s.client, m, s.getFromAddr(m))
    } else {
        err = s.singleSend(s.client, m, s.getFromAddr(m))
    }
    if err != nil {
        return
    }
    s.rate = time.Now()
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

    var body bytes.Buffer
    for k, v := range header {
        body.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
    }
    body.WriteString("\r\n")
    body.WriteString(b64.EncodeToString([]byte(m.Body)))

    return body.Bytes()
}

func (s *SmtpServer) massSend(client *smtp.Client, m *Message, sendAddr string) (err error) {
    err = s.client.Mail(sendAddr)
    if err != nil {
        if s.auth != nil {
            s.upgrade()
        }
        return
    }
    var toStr bytes.Buffer
    for name, addr := range m.To {
        to := mail.Address{name, addr}
        toStr.WriteString(to.String())
        toStr.WriteString(", ")
        if err = client.Rcpt(addr); err != nil {
            return
        }
    }
    w, err := client.Data()
    if err != nil {
        return
    }
    if _, err = w.Write(s.formatBody(m, toStr.String())); err != nil {
        return
    }
    err = w.Close()
    return
}

func (s *SmtpServer) singleSend(client *smtp.Client, m *Message, sendAddr string) (err error) {
    for name, addr := range m.To {
        err = s.client.Mail(sendAddr)
        if err != nil {
            if s.auth != nil {
                s.upgrade()
            }
            break
        }
        if err = client.Rcpt(addr); err != nil {
            break
        }
        w, err := client.Data()
        if err != nil {
            break
        }
        to := mail.Address{name, addr}
        if _, err = w.Write(s.formatBody(m, to.String())); err != nil {
            break
        }
        if err = w.Close(); err != nil {
            break
        }
    }
    return
}
