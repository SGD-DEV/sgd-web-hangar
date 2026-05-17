package dns

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/devour-app/devour/app/config"
	dnspkg "github.com/miekg/dns"
)

type Server struct {
	store    *config.Store
	server   *dnspkg.Server
	mu       sync.Mutex
	running  bool
}

func NewServer(store *config.Store) *Server {
	return &Server{
		store: store,
	}
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	cfg, err := s.store.GetAppConfig()
	if err != nil {
		return fmt.Errorf("dns: reading config: %w", err)
	}

	port := cfg.DNSPort
	if port == 0 {
		port = 53
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)

	handler := dnspkg.NewServeMux()
	handler.HandleFunc(".", s.handleRequest)

	s.server = &dnspkg.Server{
		Addr:    addr,
		Net:     "udp",
		Handler: handler,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil {
			if s.running {
				fmt.Printf("dns: server error: %v\n", err)
			}
		}
	}()

	s.running = true
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		s.running = false
		return nil
	}

	s.running = false
	err := s.server.Shutdown()
	s.server = nil
	return err
}

func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Server) handleRequest(w dnspkg.ResponseWriter, r *dnspkg.Msg) {
	m := new(dnspkg.Msg)
	m.SetReply(r)
	m.Authoritative = true

	for _, q := range r.Question {
		name := strings.ToLower(q.Name)

		if q.Qtype == dnspkg.TypeA && strings.HasSuffix(name, ".test.") {
			rr := &dnspkg.A{
				Hdr: dnspkg.RR_Header{
					Name:   q.Name,
					Rrtype: dnspkg.TypeA,
					Class:  dnspkg.ClassINET,
					Ttl:    60,
				},
				A: net.ParseIP("127.0.0.1"),
			}
			m.Answer = append(m.Answer, rr)
		} else {
			s.forwardQuery(w, r)
			return
		}
	}

	w.WriteMsg(m)
}

func (s *Server) forwardQuery(w dnspkg.ResponseWriter, r *dnspkg.Msg) {
	c := new(dnspkg.Client)
	resp, _, err := c.Exchange(r, "8.8.8.8:53")
	if err != nil {
		m := new(dnspkg.Msg)
		m.SetRcode(r, dnspkg.RcodeServerFailure)
		w.WriteMsg(m)
		return
	}
	w.WriteMsg(resp)
}
