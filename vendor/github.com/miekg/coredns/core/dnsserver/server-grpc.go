package dnsserver

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"

	"github.com/miekg/dns"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"

	"github.com/coredns/coredns/pb"
)

// servergRPC represents an instance of a DNS-over-gRPC server.
type servergRPC struct {
	*Server
	grpcServer *grpc.Server

	listenAddr net.Addr
}

// NewGRPCServer returns a new CoreDNS GRPC server and compiles all middleware in to it.
func NewServergRPC(addr string, group []*Config) (*servergRPC, error) {

	s, err := NewServer(addr, group)
	if err != nil {
		return nil, err
	}
	gs := &servergRPC{Server: s}
	gs.grpcServer = grpc.NewServer()
	// trace foo... TODO(miek)
	pb.RegisterDnsServiceServer(gs.grpcServer, gs)

	return gs, nil
}

// Serve implements caddy.TCPServer interface.
func (s *servergRPC) Serve(l net.Listener) error {
	s.m.Lock()
	s.listenAddr = l.Addr()
	s.m.Unlock()

	return s.grpcServer.Serve(l)
}

// ServePacket implements caddy.UDPServer interface.
func (s *servergRPC) ServePacket(p net.PacketConn) error { return nil }

// Listen implements caddy.TCPServer interface.
func (s *servergRPC) Listen() (net.Listener, error) {

	// The *tls* middleware must make sure that multiple conflicting
	// TLS configuration return an error: it can only be specified once.
	tlsConfig := new(tls.Config)
	for _, conf := range s.zones {
		// Should we error if some configs *don't* have TLS?
		tlsConfig = conf.TLSConfig
	}

	var (
		l   net.Listener
		err error
	)

	if tlsConfig == nil {
		l, err = net.Listen("tcp", s.Addr[len(TransportGRPC+"://"):])
	} else {
		l, err = tls.Listen("tcp", s.Addr[len(TransportGRPC+"://"):], tlsConfig)
	}

	if err != nil {
		return nil, err
	}
	return l, nil
}

// ListenPacket implements caddy.UDPServer interface.
func (s *servergRPC) ListenPacket() (net.PacketConn, error) { return nil, nil }

// OnStartupComplete lists the sites served by this server
// and any relevant information, assuming Quiet is false.
func (s *servergRPC) OnStartupComplete() {
	if Quiet {
		return
	}

	for zone, config := range s.zones {
		fmt.Println(TransportGRPC + "://" + zone + ":" + config.Port)
	}
}

func (s *servergRPC) Stop() (err error) {
	s.m.Lock()
	defer s.m.Unlock()
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	return
}

// Query is the main entry-point into the gRPC server. From here we call ServeDNS like
// any normal server. We use a custom responseWriter to pick up the bytes we need to write
// back to the client as a protobuf.
func (s *servergRPC) Query(ctx context.Context, in *pb.DnsPacket) (*pb.DnsPacket, error) {
	msg := new(dns.Msg)
	err := msg.Unpack(in.Msg)
	if err != nil {
		return nil, err
	}

	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("no peer in gRPC context")
	}

	a, ok := p.Addr.(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("no TCP peer in gRPC context: %v", p.Addr)
	}

	r := &net.IPAddr{IP: a.IP}
	w := &gRPCresponse{localAddr: s.listenAddr, remoteAddr: r, Msg: msg}

	s.ServeDNS(ctx, w, msg)

	packed, err := w.Msg.Pack()
	if err != nil {
		return nil, err
	}

	return &pb.DnsPacket{Msg: packed}, nil
}

func (g *servergRPC) Shutdown() error {
	if g.grpcServer != nil {
		g.grpcServer.Stop()
	}
	return nil
}

type gRPCresponse struct {
	localAddr  net.Addr
	remoteAddr net.Addr
	Msg        *dns.Msg
}

// Write is the hack that makes this work. It does not actually write the message
// but returns the bytes we need to to write in r. We can then pick this up in Query
// and write a proper protobuf back to the client.
func (r *gRPCresponse) Write(b []byte) (int, error) {
	r.Msg = new(dns.Msg)
	return len(b), r.Msg.Unpack(b)
}

// These methods implement the dns.ResponseWriter interface from Go DNS.
func (r *gRPCresponse) Close() error              { return nil }
func (r *gRPCresponse) TsigStatus() error         { return nil }
func (r *gRPCresponse) TsigTimersOnly(b bool)     { return }
func (r *gRPCresponse) Hijack()                   { return }
func (r *gRPCresponse) LocalAddr() net.Addr       { return r.localAddr }
func (r *gRPCresponse) RemoteAddr() net.Addr      { return r.remoteAddr }
func (r *gRPCresponse) WriteMsg(m *dns.Msg) error { r.Msg = m; return nil }
