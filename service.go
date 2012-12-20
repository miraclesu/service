package service

import (
    "time"
)

const (
    Limit = 10 * time.Minute
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
    running    bool
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

func (s *Service) RemoveServer(server Server) (err error) {
    b := false
    for k, v := range s.serverlist[server.Tag()] {
        if v == server {
            s.serverlist[server.Tag()] = append(s.serverlist[server.Tag()][:k],
                s.serverlist[server.Tag()][k+1:]...)
            b = true
        }
    }
    if !b {
        return Errorf("The server does not exist: %+v", server)
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
    s.running = true
}

func (s *Service) Close() {
    s.running = false
    close(s.origMsg)
    close(s.msg)
}

func (s *Service) Send(m *Message) error {
    if !s.running {
        return ErrServiceNoRunning
    }
    go func() {
        s.origMsg <- m
    }()
    return nil
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

func (s *Service) removeServer(server Server) (err error) {
    if server.Rate().Sub(time.Now()) > Limit {
        return s.RemoveServer(server)
    }
    return
}

func (s *Service) selectServer(tag string) (server Server, err error) {
ReSelect:
    servers := s.serverlist[tag]
    if len(servers) == 0 {
        return nil, Errorf("No active %s server.", tag)
    }
    server = servers[0]
    for i := len(servers) - 1; i >= 0; i-- {
        if servers[i].Rate().Sub(time.Now()) > Limit {
            s.RemoveServer(servers[i])
            goto ReSelect
        }
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
    for m := range s.msg {
        server, err := s.selectServer(m.Tag)
        if err == nil {
            err = server.Send(m)
        }
        if err != nil {
            s.err(err)
            s.err(Errorf("Failed to send the message : %+v"))
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
