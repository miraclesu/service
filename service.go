package service

import (
    // "fmt"
    "time"
)

type Server interface {
    Rate() time.Time
    Tag() string
    Running() bool
    Send(m *Message) error
    Close(t time.Duration) error
}

type Message struct {
    SenderName string
    From       string
    To         map[string]string
    Subject    string
    Body       []byte
    Tag        string
    Mass       bool
}

type ServerList map[string][]Server

type Service struct {
    serverlist ServerList
    origMsg    chan *Message
    msg        chan *Message
    ErrHandler ErrorHandler
}

func New() (s *Service) {
    s = &Service{
        serverlist: make(map[string][]Server),
        origMsg:    make(chan *Message, 512),
        msg:        make(chan *Message, 1024),
    }
    return
}

func (s *Service) AddServer(server Server) {
    s.serverlist[server.Tag()] = append(s.serverlist[server.Tag()], server)
}

func (s *Service) RemoveServer(server Server) (b bool) {
    for k, v := range s.serverlist[server.Tag()] {
        if v == server {
            s.serverlist[server.Tag()] = append(s.serverlist[server.Tag()][:k],
                s.serverlist[server.Tag()][k+1:]...)
            b = true
        }
    }
    if len(s.serverlist[server.Tag()]) == 0 {
        s.err(ErrNoActiveServer)
    }
    return
}

func (s *Service) Work(t time.Duration) {
    go s.split()
    go s.sendLoop()
    go s.execTimeOut(t)
}

func (s *Service) split() {
    for m := range s.origMsg {
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

func (s *Service) noMsg() bool {
    return len(s.msg) == 0 && len(s.origMsg) == 0
}

func (s *Service) selectServer(tag string) (server Server) {
    servers := s.serverlist[tag]
    //TODO servers' len is 0 ?
    server = servers[0]
    for i := len(servers) - 1; i > 0; i-- {
        if server.Rate().After(servers[i].Rate()) {
            if len(s.msg) < len(servers) && server.Running() && !servers[i].Running() {
                continue
            }
            server = servers[i]
        }
    }
    return
}

func (s *Service) sendLoop() {
    var server Server
    for m := range s.msg {
        server = s.selectServer(m.Tag)
        err := server.Send(m)
        if err != nil {
            s.err(err)
        }
    }
}

func (s *Service) execTimeOut(t time.Duration) {
    c := time.Tick(1 * time.Second)
    for _ = range c {
        for _, servers := range s.serverlist {
            for _, server := range servers {
                if time.Now().Sub(server.Rate()) > t {
                    err := server.Close(t)
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
