package net

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Leoj030/go-luau/luau"
	"github.com/ceilo-org/ceilo/internal/stdlib/task"
)

const (
	maxBody        = 10 << 20
	maxHeaderBytes = 1 << 20
	maxRequestLine = 8 << 10
	maxHeaderLine  = 8 << 10
	readTimeout    = 10 * time.Second
	writeTimeout   = 10 * time.Second
)

type request struct {
	Method  string            `luau:"method"`
	Path    string            `luau:"path"`
	Query   map[string]string `luau:"query"`
	Headers map[string]string `luau:"headers"`
	Body    string            `luau:"body"`
}

type response struct {
	status  int
	headers map[string]string
	body    string
}

type responseValue struct {
	Status  int               `luau:"status"`
	Headers map[string]string `luau:"headers"`
	Body    string            `luau:"body"`
}

type options struct {
	Hostname    string      `luau:"hostname"`
	Port        *int        `luau:"port"`
	ReusePort   bool        `luau:"reuseport"`
	MaxBodySize *int        `luau:"maxbodysize"`
	TLS         *tlsOptions `luau:"tls"`
}

type tlsOptions struct {
	CertFilename string  `luau:"certfilename"`
	KeyFilename  string  `luau:"keyfilename"`
	Passphrase   *string `luau:"passphrase"`
	CAFilename   string  `luau:"cafilename"`
}

type result struct {
	response response
	err      error
}

type module struct {
	sched  *task.Scheduler
	L      *luau.State
	ctx    context.Context
	logErr func(error)
}

type server struct {
	m          *module
	handlerRef luau.Ref
	objectRef  luau.Ref
	listener   net.Listener

	mu          sync.Mutex
	conns       map[net.Conn]struct{}
	closing     bool
	closeOnce   sync.Once
	closed      chan struct{}
	wg          sync.WaitGroup
	maxBodySize int
}

func (m *module) serve(c *luau.State) (int, error) {
	optionsArg, handlerArg := 1, 0
	switch c.TypeName(2) {
	case "function":
		handlerArg = 2
	case "table":
		optionsArg, handlerArg = 2, 0
		if c.TypeName(3) == "function" {
			handlerArg = 3
		}
	}

	if c.TypeName(optionsArg) != "table" {
		return 0, errors.New("net.server: configuration must be a table")
	}

	opts := options{Hostname: "127.0.0.1"}
	if err := c.Decode(optionsArg, &opts); err != nil {
		return 0, fmt.Errorf("net.server: configuration: %w", err)
	}
	if opts.Port == nil {
		defaultPort := 8080
		opts.Port = &defaultPort
	}
	if *opts.Port < 0 || *opts.Port > 65535 {
		return 0, fmt.Errorf("net.server: configuration.port %d is out of range", *opts.Port)
	}

	maxBodySize := maxBody
	if opts.MaxBodySize != nil {
		if *opts.MaxBodySize < 0 {
			return 0, errors.New("net.server: configuration.maxbodysize must not be negative")
		}
		maxBodySize = *opts.MaxBodySize
	}

	if handlerArg == 0 {
		if err := c.GetField(optionsArg, "handler"); err != nil {
			return 0, err
		}
		if c.TypeName(-1) != "function" {
			if err := c.Pop(1); err != nil {
				return 0, err
			}
			return 0, errors.New("net.server: configuration.handler must be a function")
		}
		handlerArg = -1
	} else if c.TypeName(handlerArg) != "function" {
		return 0, errors.New("net.server: handler must be a function")
	}
	ref, err := c.Ref(handlerArg)
	if handlerArg == -1 {
		if popErr := c.Pop(1); err == nil {
			err = popErr
		}
	}
	if err != nil {
		return 0, err
	}

	address := net.JoinHostPort(opts.Hostname, strconv.Itoa(*opts.Port))
	listener, err := listenTCP(address, opts.ReusePort)
	if err != nil {
		c.Unref(ref)
		return 0, fmt.Errorf("net.server: %w", err)
	}

	if opts.TLS != nil {
		config, err := loadTLSConfig(opts.TLS)
		if err != nil {
			c.Unref(ref)
			_ = listener.Close()
			return 0, fmt.Errorf("net.server: tls: %w", err)
		}
		listener = tls.NewListener(listener, config)
	}

	srv := &server{
		m:           m,
		handlerRef:  ref,
		listener:    listener,
		maxBodySize: maxBodySize,
		conns:       make(map[net.Conn]struct{}),
		closed:      make(chan struct{}),
	}

	if err := c.NewTable(0, 2); err != nil {
		c.Unref(ref)
		_ = listener.Close()
		return 0, err
	}
	if err := c.Push(listener.Addr().(*net.TCPAddr).Port); err != nil {
		c.Unref(ref)
		_ = listener.Close()
		return 0, err
	}
	if err := c.SetField(-2, "port"); err != nil {
		c.Unref(ref)
		_ = listener.Close()
		return 0, err
	}
	if err := c.PushFunction("close", srv.close); err != nil {
		c.Unref(ref)
		_ = listener.Close()
		return 0, err
	}
	if err := c.SetField(-2, "close"); err != nil {
		c.Unref(ref)
		_ = listener.Close()
		return 0, err
	}
	srv.objectRef, err = c.Ref(-1)
	if err != nil {
		c.Unref(ref)
		_ = listener.Close()
		return 0, err
	}

	if err := m.sched.Hold(); err != nil {
		c.Unref(srv.objectRef)
		c.Unref(ref)
		_ = listener.Close()
		return 0, err
	}
	go srv.accept()
	go func() {
		select {
		case <-m.ctx.Done():
			srv.shutdown()
		case <-srv.done():
		}
	}()
	return 1, nil
}

func (s *server) done() <-chan struct{} {
	// Listener closure is the server's shutdown signal.
	return s.closed
}

func (s *server) close(*luau.State) (int, error) {
	s.shutdown()
	return 0, nil
}

func (s *server) shutdown() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		conns := make([]net.Conn, 0, len(s.conns))
		for conn := range s.conns {
			conns = append(conns, conn)
		}
		s.mu.Unlock()

		_ = s.listener.Close()
		for _, conn := range conns {
			_ = conn.Close()
		}
		go func() {
			s.wg.Wait()
			if err := s.m.sched.Post(func() {
				s.m.L.Unref(s.handlerRef)
				s.m.L.Unref(s.objectRef)
				if err := s.m.sched.Release(); err != nil {
					s.m.report(fmt.Errorf("net.server: release scheduler hold: %w", err))
				}
			}); err != nil {
				log.Printf("net.server: cleanup: %v", err)
			}
		}()
	})
}

func (s *server) accept() {
	defer close(s.closed)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if !closing {
				s.m.postError(fmt.Errorf("net.server: accept: %w", err))
				s.shutdown()
			}
			return
		}

		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		s.conns[conn] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()

		go func() {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
				_ = conn.Close()
			}()
			s.handle(conn)
		}()
	}
}

func (s *server) handle(conn net.Conn) {
	reader := bufio.NewReaderSize(conn, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		req, keepAlive, status, err := readRequestLimit(reader, s.maxBodySize)
		if err != nil {
			if status != 0 {
				_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
				_ = writeResponse(conn, response{status: status, body: statusText(status)}, false, false)
			} else if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.m.postError(fmt.Errorf("net.server: read request: %w", err))
			}
			return
		}
		_ = conn.SetDeadline(time.Time{})

		resp, err := s.callHandler(req)
		if err != nil {
			s.m.postError(fmt.Errorf("net.server: handler: %w", err))
			resp = response{status: 500, body: "internal server error"}
			keepAlive = false
		}

		_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err := writeResponse(conn, resp, keepAlive, req.Method == "HEAD"); err != nil {
			return
		}
		if !keepAlive {
			return
		}
	}
}

func (s *server) callHandler(req *request) (response, error) {
	encoded, err := luau.Encode(req)
	if err != nil {
		return response{}, err
	}

	reply := make(chan result, 1)
	if err := s.m.sched.Submit(s.m.L, func(th *luau.Thread) (int, error) {
		if err := th.PushRef(s.handlerRef); err != nil {
			return 0, err
		}
		if err := th.PushEncoded(encoded); err != nil {
			return 0, err
		}
		if err := th.PushRef(s.objectRef); err != nil {
			return 0, err
		}
		return 2, nil
	}, func(th *luau.Thread, err error) {
		if err != nil {
			reply <- result{err: err}
			return
		}
		resp, err := toResponse(th)
		reply <- result{response: resp, err: err}
	}); err != nil {
		return response{}, err
	}

	select {
	case out := <-reply:
		return out.response, out.err
	case <-s.m.ctx.Done():
		return response{}, s.m.ctx.Err()
	}
}

func (m *module) report(err error) {
	if m.logErr != nil {
		m.logErr(err)
		return
	}
	log.Printf("%v", err)
}

func (m *module) postError(err error) {
	if postErr := m.sched.Post(func() { m.report(err) }); postErr != nil {
		log.Printf("%v (unable to report on task scheduler: %v)", err, postErr)
	}
}

func toResponse(th *luau.Thread) (response, error) {
	switch th.TypeName(1) {
	case "string":
		value, err := th.Get(1)
		if err != nil {
			return response{}, err
		}
		return response{status: 200, body: value.(string)}, nil
	case "table":
		var value responseValue
		if err := th.Decode(1, &value); err != nil {
			return response{}, fmt.Errorf("response: %w", err)
		}
		resp := response{status: value.Status, headers: value.Headers, body: value.Body}
		if resp.status == 0 {
			resp.status = 200
		}
		if resp.status < 100 || resp.status > 599 {
			return response{}, fmt.Errorf("response status %d is out of range", resp.status)
		}
		for key, header := range resp.headers {
			if !validHeaderName(key) || !validHeaderValue(header) {
				return response{}, fmt.Errorf("response contains invalid header %q", key)
			}
		}
		return resp, nil
	default:
		return response{}, fmt.Errorf("handler must return a string or response table, got %s", th.TypeName(1))
	}
}

func readRequest(r *bufio.Reader) (*request, bool, int, error) {
	return readRequestLimit(r, maxBody)
}

func readRequestLimit(r *bufio.Reader, maxBodySize int) (*request, bool, int, error) {
	line, err := readLine(r, maxRequestLine)
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) == 0 {
			return nil, false, 0, io.EOF
		}
		return nil, false, 400, err
	}
	line = trimLineEnding(line)
	parts := strings.Split(string(line), " ")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, false, 400, errors.New("malformed request line")
	}
	if parts[2] != "HTTP/1.1" && parts[2] != "HTTP/1.0" {
		return nil, false, 400, fmt.Errorf("unsupported protocol %q", parts[2])
	}
	if !validHeaderName(parts[0]) {
		return nil, false, 400, errors.New("invalid request method")
	}
	target, err := url.ParseRequestURI(parts[1])
	if err != nil {
		return nil, false, 400, fmt.Errorf("invalid request target: %w", err)
	}
	query, err := url.ParseQuery(target.RawQuery)
	if err != nil {
		return nil, false, 400, fmt.Errorf("invalid query: %w", err)
	}

	req := &request{
		Method:  parts[0],
		Path:    target.Path,
		Query:   make(map[string]string, len(query)),
		Headers: make(map[string]string),
	}
	for key, values := range query {
		if len(values) > 0 {
			req.Query[key] = values[0]
		}
	}

	headerBytes := 0
	contentLength := int64(-1)
	transferEncoding := ""
	hasTransferEncoding := false
	for {
		line, err = readLine(r, maxHeaderLine)
		headerBytes += len(line)
		if headerBytes > maxHeaderBytes {
			return nil, false, 431, errors.New("request headers are too large")
		}
		if err != nil {
			return nil, false, 400, err
		}
		line = trimLineEnding(line)
		if len(line) == 0 {
			break
		}
		if line[0] == ' ' || line[0] == '\t' {
			return nil, false, 400, errors.New("obsolete folded header")
		}
		colon := bytes.IndexByte(line, ':')
		if colon <= 0 {
			return nil, false, 400, errors.New("malformed request header")
		}
		name := string(line[:colon])
		value := strings.TrimSpace(string(line[colon+1:]))
		if !validHeaderName(name) || !validHeaderValue(value) {
			return nil, false, 400, fmt.Errorf("invalid request header %q", name)
		}
		name = strings.ToLower(name)
		if existing, ok := req.Headers[name]; ok {
			req.Headers[name] = existing + ", " + value
		} else {
			req.Headers[name] = value
		}
		switch name {
		case "content-length":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return nil, false, 400, errors.New("invalid content-length")
			}
			if contentLength >= 0 && contentLength != n {
				return nil, false, 400, errors.New("conflicting content-length headers")
			}
			contentLength = n
		case "transfer-encoding":
			if hasTransferEncoding {
				return nil, false, 400, errors.New("multiple transfer-encoding headers")
			}
			if value == "" {
				return nil, false, 400, errors.New("empty transfer-encoding")
			}
			transferEncoding = strings.ToLower(value)
			hasTransferEncoding = true
		}
	}

	if parts[2] == "HTTP/1.1" && req.Headers["host"] == "" {
		return nil, false, 400, errors.New("missing host header")
	}
	if req.Headers["expect"] != "" {
		return nil, false, 417, errors.New("request expectations are not supported")
	}
	if transferEncoding != "" {
		if contentLength >= 0 {
			return nil, false, 400, errors.New("content-length and transfer-encoding cannot be combined")
		}
		if transferEncoding != "chunked" {
			return nil, false, 501, errors.New("unsupported transfer-encoding")
		}
		req.Body, err = readChunkedBody(r, &headerBytes, maxBodySize)
		if err != nil {
			if errors.Is(err, errBodyTooLarge) {
				return nil, false, 413, err
			}
			return nil, false, 400, err
		}
	} else if contentLength > 0 {
		if contentLength > int64(maxBodySize) {
			return nil, false, 413, errBodyTooLarge
		}
		body := make([]byte, int(contentLength))
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, false, 400, fmt.Errorf("read request body: %w", err)
		}
		req.Body = string(body)
	}

	keepAlive := parts[2] == "HTTP/1.1"
	connection := strings.ToLower(req.Headers["connection"])
	if hasToken(connection, "close") {
		keepAlive = false
	} else if parts[2] == "HTTP/1.0" && hasToken(connection, "keep-alive") {
		keepAlive = true
	}
	return req, keepAlive, 0, nil
}

var errBodyTooLarge = errors.New("request body is too large")

func readChunkedBody(r *bufio.Reader, headerBytes *int, maxBodySize int) (string, error) {
	body := make([]byte, 0, 1024)
	for {
		line, err := readLine(r, maxHeaderLine)
		*headerBytes += len(line)
		if *headerBytes > maxHeaderBytes {
			return "", errors.New("request headers and trailers are too large")
		}
		if err != nil {
			return "", err
		}
		chunkSizeText := strings.TrimSpace(strings.SplitN(string(trimLineEnding(line)), ";", 2)[0])
		size, err := strconv.ParseUint(chunkSizeText, 16, 63)
		if err != nil {
			return "", errors.New("invalid chunk size")
		}
		if size == 0 {
			for {
				trailer, err := readLine(r, maxHeaderLine)
				*headerBytes += len(trailer)
				if *headerBytes > maxHeaderBytes {
					return "", errors.New("request headers and trailers are too large")
				}
				if err != nil {
					return "", err
				}
				if len(trimLineEnding(trailer)) == 0 {
					return string(body), nil
				}
				trailer = trimLineEnding(trailer)
				colon := bytes.IndexByte(trailer, ':')
				if colon <= 0 ||
					!validHeaderName(string(trailer[:colon])) ||
					!validHeaderValue(strings.TrimSpace(string(trailer[colon+1:]))) {
					return "", errors.New("malformed request trailer")
				}
			}
		}
		if size > uint64(maxBodySize-len(body)) {
			return "", errBodyTooLarge
		}
		start := len(body)
		body = append(body, make([]byte, int(size))...)
		if _, err := io.ReadFull(r, body[start:]); err != nil {
			return "", fmt.Errorf("read chunk: %w", err)
		}
		ending := make([]byte, 2)
		if _, err := io.ReadFull(r, ending); err != nil || !bytes.Equal(ending, []byte("\r\n")) {
			return "", errors.New("invalid chunk ending")
		}
	}
}

func readLine(r *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > limit {
			return nil, errors.New("request line or header is too long")
		}
		line = append(line, part...)
		if err == nil {
			return line, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

func trimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r"))
}

func writeResponse(w io.Writer, resp response, keepAlive, head bool) error {
	if resp.status == 0 {
		resp.status = 200
	}
	if resp.status < 100 || resp.status > 599 {
		return fmt.Errorf("invalid response status %d", resp.status)
	}

	body := resp.body
	if head || resp.status == 204 || resp.status == 304 {
		body = ""
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "HTTP/1.1 %d %s\r\n", resp.status, statusText(resp.status))

	keys := make([]string, 0, len(resp.headers))
	for key, value := range resp.headers {
		if !validHeaderName(key) || !validHeaderValue(value) {
			return fmt.Errorf("invalid response header %q", key)
		}
		switch strings.ToLower(key) {
		case "content-length", "transfer-encoding", "connection":
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&out, "%s: %s\r\n", key, resp.headers[key])
	}
	if resp.status != 204 {
		fmt.Fprintf(&out, "Content-Length: %d\r\n", len(resp.body))
	}
	if keepAlive {
		out.WriteString("Connection: keep-alive\r\n")
	} else {
		out.WriteString("Connection: close\r\n")
	}
	out.WriteString("\r\n")
	out.WriteString(body)
	return writeAll(w, out.Bytes())
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c))) {
			return false
		}
	}
	return true
}

func validHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		if (value[i] < 0x20 && value[i] != '\t') || value[i] == 0x7f {
			return false
		}
	}
	return true
}

func hasToken(value, token string) bool {
	for part := range strings.SplitSeq(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func statusText(status int) string {
	switch status {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 204:
		return "No Content"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 304:
		return "Not Modified"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 413:
		return "Payload Too Large"
	case 415:
		return "Unsupported Media Type"
	case 417:
		return "Expectation Failed"
	case 431:
		return "Request Header Fields Too Large"
	case 500:
		return "Internal Server Error"
	case 501:
		return "Not Implemented"
	case 503:
		return "Service Unavailable"
	default:
		return "Status"
	}
}
