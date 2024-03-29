package service

import (
    "labix.org/v2/mgo/bson"
    "time"
)

const (
    ErrTimes  = 3
    TickStep  = 20 * time.Second
    Unlimited = 0
)

type Server interface {
    Init(conf ...interface{}) error
    Send(m *Message) error
    Close() error
    Rate() time.Time
    Tag() string
    Running() bool
    Timeout() bool
    Sick() bool
}

type Message struct {
    Id         bson.ObjectId
    SenderName string
    From       string
    To         map[string]string
    Subject    string
    Body       string
    Tag        string
    Mass       bool
    Times      int
}

type ServerList map[string][]Server

type Service struct {
    serverlist ServerList
    origMsg    chan *Message
    msg        chan *Message
    limit      chan bool
    running    bool
    ErrHandler ErrorHandler
    MsgHandler MessageHandler
}

func New(l int) (s *Service) {
    s = &Service{
        serverlist: make(map[string][]Server),
        origMsg:    make(chan *Message, 128),
        msg:        make(chan *Message, 256),
    }
    if l != Unlimited {
        s.limit = make(chan bool, l)
        for i := 0; i < l; i++ {
            s.limit <- true
        }
    }
    return
}

func (s *Service) AddServer(server Server, conf ...interface{}) (err error) {
    for _, v := range s.serverlist[server.Tag()] {
        if v == server {
            return Errorf("The server already exists: %v", server)
        }
    }
    err = server.Init(conf...)
    if err != nil {
        return
    }
    s.serverlist[server.Tag()] = append(s.serverlist[server.Tag()], server)
    return
}

func (s *Service) RemoveServer(server Server) {
    b := false
    for k, v := range s.serverlist[server.Tag()] {
        if v == server {
            s.serverlist[server.Tag()] = append(s.serverlist[server.Tag()][:k],
                s.serverlist[server.Tag()][k+1:]...)
            b = true
        }
    }
    if !b {
        s.err(Errorf("The server does not exist: %+v", server))
    }
    if len(s.serverlist[server.Tag()]) == 0 {
        s.err(ErrNoActiveServer)
    }
}

func (s *Service) removeSickServer(tag string) {
    for _, server := range s.serverlist[tag] {
        if server.Sick() {
            s.RemoveServer(server)
        }
    }
}

func (s *Service) Work() {
    go s.split()
    go s.sendLoop()
    go s.execTimeOut()
    s.running = true
}

func (s *Service) Close() {
    s.running = false
    close(s.origMsg)
    close(s.msg)
    if s.limit != nil {
        close(s.limit)
    }
}

func (s *Service) Send(m *Message) error {
    if !s.running {
        s.err(ErrServiceWarning)
    }
    if len(s.serverlist[m.Tag]) == 0 && len(s.origMsg) > cap(s.origMsg)/2 {
        return ErrNoActiveServer
    }
    m.Id = bson.NewObjectId()
    s.origMsg <- m
    return nil
}

func (s *Service) split() {
    for m := range s.origMsg {
        if len(m.To) == 0 {
            s.err(Errorf("Error: Failed to send the message : %+v, Message need \"To\" field.", m))
        }
        if m.Mass {
            s.msg <- m
        } else {
            for k, v := range m.To {
                msg := *m
                msg.To = map[string]string{k: v}
                s.msg <- &msg
            }
        }
    }
}

func (s *Service) selectServer(tag string) (server Server, err error) {
    s.removeSickServer(tag)
    servers := s.serverlist[tag]
    if len(servers) == 0 {
        return nil, Errorf("No active %s server.", tag)
    }
    server = servers[0]
    for i := len(servers) - 1; i >= 0; i-- {
        if server.Rate().After(servers[i].Rate()) {
            if len(s.msg) < len(servers) && server.Running() && !servers[i].Running() {
                continue
            }
            server = servers[i]
        }
    }
    return
}

func (s *Service) send(server Server, m *Message) {
    if s.limit != nil {
        defer func() {
            s.limit <- true
        }()
    }
    if err := server.Send(m); err != nil {
        s.err(err)
        if m.Times < ErrTimes {
            m.Times++
            s.msg <- m
        } else {
            s.err(Errorf("Failed to send the message : %+v", m))
        }
    } else {
        s.msgf(Errorf("Successfully send the message : %+v", m))
    }
}

func (s *Service) sendLoop() {
    for m := range s.msg {
        if s.limit != nil {
            <-s.limit
        }
        server, err := s.selectServer(m.Tag)
        if err != nil {
            s.err(err)
            continue
        }
        go s.send(server, m)
    }
}

func (s *Service) execTimeOut() {
    c := time.Tick(TickStep)
    for _ = range c {
        for _, servers := range s.serverlist {
            for _, server := range servers {
                if server.Timeout() {
                    err := server.Close()
                    if err != nil {
                        s.err(err)
                    }
                }
            }
        }
    }
}

func (s *Service) err(e error) {
    if s.ErrHandler != nil {
        s.ErrHandler(e)
    }
}

func (s *Service) msgf(e error) {
    if s.MsgHandler != nil {
        s.MsgHandler(e)
    }
}
