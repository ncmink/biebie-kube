package ipc

import (
	stdctx "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"biebie-kube/protocol/version"
)

// Method names the operation a peer is asking for. Keeping them as constants
// means a typo is a compile error rather than a silent "unknown method".
type Method string

// The operations this protocol version defines.
const (
	// MethodPing is a liveness check used to decide whether the other
	// application is running before offering its buttons.
	MethodPing Method = "ping"

	// MethodConsumeHandoff redeems a handoff ticket for a Biebie Context.
	MethodConsumeHandoff Method = "handoff.consume"

	// MethodAccessStatus asks Biebie Access whether a profile is connected.
	MethodAccessStatus Method = "access.status"

	// MethodAccessConnect asks Biebie Access to bring a profile up, which
	// surfaces its window; it never performs an unattended login.
	MethodAccessConnect Method = "access.connect"

	// MethodAccessSessionChanged notifies a peer that a profile's state moved,
	// so a waiting cluster can retry without a restart.
	MethodAccessSessionChanged Method = "access.session.changed"
)

// Limits chosen so a malformed or hostile peer cannot exhaust this process.
const (
	maxMessageBytes = 1 << 20 // 1 MiB
	readTimeout     = 5 * time.Second
	writeTimeout    = 5 * time.Second

	// DefaultDialTimeout is short: the peer is a local process or it is not
	// there at all, and the user is waiting on a button.
	DefaultDialTimeout = 2 * time.Second
)

// Request is one call. Params is left raw so a handler decodes only what it
// expects.
type Request struct {
	version.Envelope
	Method Method          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the single reply to a Request.
type Response struct {
	version.Envelope
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// Handler serves one method. Returning an error produces a failed Response
// rather than dropping the connection, so the caller can explain the failure.
type Handler func(ctx stdctx.Context, params json.RawMessage) (any, error)

// Server accepts local connections and dispatches to registered handlers.
//
// One request per connection: there is no session, no multiplexing and no
// state to confuse, which is all this protocol needs and much less to get
// wrong.
type Server struct {
	endpoint Endpoint

	mu       sync.RWMutex
	handlers map[Method]Handler

	listener net.Listener
	closed   chan struct{}
	once     sync.Once

	// OnError observes serving failures. Handlers must not log payloads.
	OnError func(error)
}

// NewServer prepares a server for an endpoint. Nothing is bound until Start.
func NewServer(endpoint Endpoint) *Server {
	return &Server{
		endpoint: endpoint,
		handlers: make(map[Method]Handler),
		closed:   make(chan struct{}),
	}
}

// Handle registers a method. Registering twice replaces the handler, which
// keeps application wiring order-independent.
func (s *Server) Handle(method Method, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

// Start binds the endpoint and serves in the background.
func (s *Server) Start() error {
	ln, err := listen(s.endpoint)
	if err != nil {
		return err
	}
	s.listener = ln

	if _, ok := s.handlers[MethodPing]; !ok {
		s.Handle(MethodPing, func(stdctx.Context, json.RawMessage) (any, error) {
			return map[string]any{"pong": true, "version": version.Current}, nil
		})
	}

	go s.acceptLoop(ln)
	return nil
}

// Close stops serving and removes the socket.
func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		close(s.closed)
		if s.listener != nil {
			err = s.listener.Close()
		}
	})
	return err
}

// Address reports where the server listens, for diagnostics.
func (s *Server) Address() string { return s.endpoint.Address }

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			s.report(fmt.Errorf("accept: %w", err))
			continue
		}
		go s.serve(conn)
	}
}

func (s *Server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	payload, err := readFrame(conn)
	if err != nil {
		s.report(fmt.Errorf("read request: %w", err))
		return
	}

	resp := s.dispatch(payload)

	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := writeFrame(conn, resp); err != nil {
		s.report(fmt.Errorf("write response: %w", err))
	}
}

func (s *Server) dispatch(payload []byte) Response {
	var req Request
	if err := json.Unmarshal(payload, &req); err != nil {
		return failure("malformed request")
	}
	if err := version.Check(req.Envelope); err != nil {
		return failure(err.Error())
	}

	s.mu.RLock()
	handler, ok := s.handlers[req.Method]
	s.mu.RUnlock()
	if !ok {
		return failure(fmt.Sprintf("unsupported method %q", req.Method))
	}

	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), readTimeout)
	defer cancel()

	result, err := handler(ctx, req.Params)
	if err != nil {
		return failure(err.Error())
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return failure("could not encode result")
	}
	return Response{Envelope: version.NewEnvelope(), OK: true, Result: encoded}
}

func (s *Server) report(err error) {
	if s.OnError != nil {
		s.OnError(err)
	}
}

func failure(message string) Response {
	return Response{Envelope: version.NewEnvelope(), OK: false, Error: message}
}

// Client calls a single method on a peer and disconnects.
type Client struct {
	endpoint Endpoint
	timeout  time.Duration
}

// NewClient targets a peer endpoint.
func NewClient(endpoint Endpoint) *Client {
	return &Client{endpoint: endpoint, timeout: DefaultDialTimeout}
}

// WithTimeout returns a client using a different dial and I/O budget.
func (c *Client) WithTimeout(d time.Duration) *Client {
	return &Client{endpoint: c.endpoint, timeout: d}
}

// ErrPeerUnavailable means the other application is not running. Callers turn
// this into an offer to launch it, not into an error dialog.
var ErrPeerUnavailable = errors.New("biebie peer application is not running")

// Call performs one request. result may be nil when the reply is not needed.
func (c *Client) Call(ctx stdctx.Context, method Method, params any, result any) error {
	conn, err := dial(ctx, c.endpoint, c.timeout)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPeerUnavailable, err)
	}
	defer func() { _ = conn.Close() }()

	req := Request{Envelope: version.NewEnvelope(), Method: method}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encode params: %w", err)
		}
		req.Params = encoded
	}

	_ = conn.SetWriteDeadline(time.Now().Add(c.timeout))
	if err := writeFrame(conn, req); err != nil {
		return fmt.Errorf("send request: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(c.timeout))
	payload, err := readFrame(conn)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(payload, &resp); err != nil {
		return errors.New("malformed response")
	}
	if err := version.Check(resp.Envelope); err != nil {
		return err
	}
	if !resp.OK {
		return errors.New(strings.TrimSpace(resp.Error))
	}
	if result == nil || len(resp.Result) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Result, result)
}

// Available reports whether the peer answers right now.
func (c *Client) Available(ctx stdctx.Context) bool {
	return c.Call(ctx, MethodPing, nil, nil) == nil
}

// Frames are newline-delimited JSON. A length cap keeps a bad peer from
// allocating this process out of memory.
func writeFrame(w io.Writer, message any) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(encoded) > maxMessageBytes {
		return fmt.Errorf("message too large (%d bytes)", len(encoded))
	}
	encoded = append(encoded, '\n')
	_, err = w.Write(encoded)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, maxMessageBytes+1)
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, err := limited.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			if idx := indexNewline(buf); idx >= 0 {
				return buf[:idx], nil
			}
			if len(buf) > maxMessageBytes {
				return nil, errors.New("message too large")
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(buf) > 0 {
				return buf, nil
			}
			return nil, err
		}
	}
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}
