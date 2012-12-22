package service

import (
	"time"
)

const (
	ErrTimes = 3
	TickStep = 1 * time.Minute
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
	SenderName string
	From       string
	To         map[string]string
	Subject    string
	Body       []byte
	Tag        string
	Mass       bool
	Times      int
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
}

func (s *Service) Send(m *Message) error {
	if !s.running {
		s.err(ErrServiceWarning)
	}
	if len(s.serverlist[m.Tag]) == 0 && len(s.origMsg) > cap(s.origMsg)/2 {
		return ErrNoActiveServer
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
	if err := server.Send(m); err != nil {
		s.err(err)
		s.err(Errorf("Failed to send the message : %+v", m))
		if m.Times < ErrTimes {
			m.Times++
			s.msg <- m
		}
	} else {
		s.err(Errorf("Successfully send the message : %+v", m))
	}
}

func (s *Service) sendLoop() {
	for m := range s.msg {
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
