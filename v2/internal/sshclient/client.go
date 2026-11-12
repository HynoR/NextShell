// Package sshclient implements SSH transports without any UI dependency.
package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/net/proxy"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
	"io"
	"net"
	"nextshell.dev/v2/internal/model"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HostKeyError struct {
	Address, Fingerprint string
	Changed              bool
}

func (e *HostKeyError) Error() string {
	if e.Changed {
		return "主机密钥已更改，拒绝连接: " + e.Address + " " + e.Fingerprint
	}
	return "首次连接，请核对并信任主机: " + e.Address + " " + e.Fingerprint
}
func Fingerprint(key []byte) string {
	s := sha256.Sum256(key)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(s[:])
}

// MatchFingerprint ports the 1.x host-fingerprint spec, including legacy pins.
func MatchFingerprint(want string, key []byte) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	sha := sha256.Sum256(key)
	md := md5.Sum(key)
	mhex := hex.EncodeToString(md[:])
	parts := make([]string, 16)
	for i := range parts {
		parts[i] = mhex[2*i : 2*i+2]
	}
	if strings.EqualFold(want[:min(len(want), 7)], "sha256:") {
		return strings.TrimRight(want[7:], "=") == base64.RawStdEncoding.EncodeToString(sha[:])
	}
	return strings.EqualFold(want, hex.EncodeToString(sha[:])) || strings.EqualFold(want, mhex) || strings.EqualFold(want, strings.Join(parts, ":"))
}

type strAddr string

func (a strAddr) Network() string { return "tcp" }
func (a strAddr) String() string  { return string(a) }

// knownHostKeyAlgorithms limits the handshake to the key types known_hosts holds for address. Without it
// x/crypto offers its own preference order (ecdsa before ed25519), so a file written by OpenSSH that only
// has the ed25519 key is reported as a changed key. Probing with a throwaway key makes the callback list
// what it knows for the host; nil means "unknown host / no file": keep the defaults.
func knownHostKeyAlgorithms(known ssh.HostKeyCallback, address string) []string {
	if known == nil {
		return nil
	}
	probe, err := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	if err != nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(known(address, strAddr(address), probe), &keyErr) {
		return nil
	}
	var algos []string
	for _, w := range keyErr.Want {
		switch t := w.Key.Type(); t {
		case ssh.KeyAlgoRSA:
			algos = append(algos, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			algos = append(algos, t)
		}
	}
	return algos
}

type Options struct {
	KnownHostsFile   string // optional existing read-only OpenSSH host-key file
	KeepaliveTimeout time.Duration
	Connection       model.Connection
	Key              model.Key
	Proxy            *model.Proxy
	HostKey          func(address, fp string) error
	Keyboard         ssh.KeyboardInteractiveChallenge
	Timeout          time.Duration
	AgentSocket      string
}
type Session struct {
	client  *ssh.Client
	session *ssh.Session
	in      io.WriteCloser
	out     io.Reader
	pipe    *io.PipeReader
	done    chan struct{}
	once    sync.Once
	writeMu sync.Mutex
	waitMu  sync.Mutex
	waitErr error
	enc     encoding.Encoding // the host's text encoding; nil for UTF-8
}

func (s *Session) Read(b []byte) (int, error) { return s.out.Read(b) }
func (s *Session) Write(b []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.in.Write(b)
}
func (s *Session) Resize(cols, rows int) error { return s.session.WindowChange(rows, cols) }
func (s *Session) Close() error {
	var e error
	s.once.Do(func() {
		close(s.done)
		e = s.client.Close()
		s.session.Close()
		s.pipe.Close()
		s.writeMu.Lock()
		s.in.Close()
		s.writeMu.Unlock()
	})
	return e
}
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) WaitError() error      { s.waitMu.Lock(); defer s.waitMu.Unlock(); return s.waitErr }

// Dial connects and opens the interactive shell. Timeout bounds the network work: the time spent in
// the HostKey and Keyboard callbacks, where a person answers a dialog, does not count against it.
func Dial(ctx context.Context, o Options) (s *Session, err error) {
	c := o.Connection
	if e := c.Validate(); e != nil {
		return nil, e
	}
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	if o.KeepaliveTimeout <= 0 {
		o.KeepaliveTimeout = o.Timeout
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	left, resumed := o.Timeout, time.Now()
	budget := time.AfterFunc(left, func() { cancel(context.DeadlineExceeded) })
	defer budget.Stop()
	defer func() {
		if err != nil && errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
			err = fmt.Errorf("ssh: connecting to %s: i/o timeout: %w", c.Address(), context.DeadlineExceeded)
		}
	}()
	asking := func() func() { // the budget stands still while a person answers
		budget.Stop()
		left -= time.Since(resumed)
		return func() { resumed = time.Now(); budget.Reset(max(left, time.Millisecond)) }
	}
	var auth []ssh.AuthMethod
	var agentConn net.Conn
	switch c.Auth {
	case "password":
		auth = append(auth, ssh.Password(c.Secret))
	case "key":
		var signer ssh.Signer
		var e error
		if o.Key.Passphrase != "" {
			signer, e = ssh.ParsePrivateKeyWithPassphrase([]byte(o.Key.Private), []byte(o.Key.Passphrase))
		} else {
			signer, e = ssh.ParsePrivateKey([]byte(o.Key.Private))
		}
		if e != nil {
			return nil, fmt.Errorf("private key: %w", e)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	case "agent":
		sock := o.AgentSocket
		if sock == "" {
			sock = os.Getenv("SSH_AUTH_SOCK")
		}
		if sock == "" {
			return nil, errors.New("SSH_AUTH_SOCK 未设置")
		}
		var e error
		agentConn, e = (&net.Dialer{}).DialContext(ctx, "unix", sock)
		if e != nil {
			return nil, e
		}
		defer agentConn.Close()
		auth = append(auth, ssh.PublicKeysCallback(agent.NewClient(agentConn).Signers))
	case "keyboard":
		if o.Keyboard == nil {
			return nil, errors.New("keyboard-interactive 需要提示处理器")
		}
		auth = append(auth, ssh.KeyboardInteractive(func(user, instruction string, questions []string, echo []bool) ([]string, error) {
			defer asking()()
			return o.Keyboard(user, instruction, questions, echo)
		}))
	}
	var known ssh.HostKeyCallback
	if o.KnownHostsFile != "" && c.Fingerprint == "" {
		var err error
		known, err = knownhosts.New(o.KnownHostsFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read known_hosts: %w", err)
		}
	}
	cfg := &ssh.ClientConfig{User: c.User, Auth: auth, Timeout: o.Timeout, HostKeyAlgorithms: knownHostKeyAlgorithms(known, c.Address()), HostKeyCallback: func(host string, remote net.Addr, k ssh.PublicKey) error {
		fp := Fingerprint(k.Marshal())
		if c.Fingerprint != "" {
			if !MatchFingerprint(c.Fingerprint, k.Marshal()) {
				return &HostKeyError{c.Address(), fp, true}
			}
			return nil
		}
		if known != nil {
			err := known(host, remote, k)
			if err == nil {
				return nil
			}
			var keyErr *knownhosts.KeyError
			if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
				return err
			}
		}
		if o.HostKey == nil {
			return &HostKeyError{c.Address(), fp, false}
		}
		defer asking()()
		return o.HostKey(c.Address(), fp)
	}}
	conn, e := dialTransport(ctx, c.Address(), o.Proxy)
	if e != nil {
		return nil, e
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	cc, chans, reqs, e := ssh.NewClientConn(conn, c.Address(), cfg)
	defer stop()
	if e != nil {
		conn.Close()
		return nil, e
	}
	client := ssh.NewClient(cc, chans, reqs)
	ss, e := client.NewSession()
	if e != nil {
		client.Close()
		return nil, e
	}
	in, e := ss.StdinPipe()
	if e != nil {
		ss.Close()
		client.Close()
		return nil, e
	}
	r, w := io.Pipe()
	ss.Stdout = w
	ss.Stderr = w
	if e = ss.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}); e != nil {
		r.Close()
		w.Close()
		ss.Close()
		client.Close()
		return nil, e
	}
	if e = ss.Shell(); e != nil {
		r.Close()
		w.Close()
		ss.Close()
		client.Close()
		return nil, e
	}
	s = &Session{client: client, session: ss, in: in, out: r, pipe: r, done: make(chan struct{})}
	if enc := Encoding(c.Encoding); enc != nil {
		s.enc = enc
		s.out = transform.NewReader(r, enc.NewDecoder())
		s.in = &encodedWriter{Writer: transform.NewWriter(in, enc.NewEncoder()), closer: in}
	}
	go func() {
		e := ss.Wait()
		s.waitMu.Lock()
		s.waitErr = e
		s.waitMu.Unlock()
		w.CloseWithError(e)
		s.Close()
	}()
	if c.Keepalive > 0 {
		go func() {
			t := time.NewTicker(time.Duration(c.Keepalive) * time.Second)
			defer t.Stop()
			for {
				select {
				case <-s.done:
					return
				case <-t.C:
					reply := make(chan error, 1)
					go func() { _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); reply <- err }()
					timer := time.NewTimer(o.KeepaliveTimeout)
					select {
					case <-s.done:
						timer.Stop()
						return
					case <-timer.C:
						s.Close()
						return
					case err := <-reply:
						timer.Stop()
						if err != nil {
							s.Close()
							return
						}
					}
				}
			}
		}()
	}
	return s, nil
}

type encodedWriter struct {
	*transform.Writer
	closer io.Closer
}

func (w *encodedWriter) Close() error {
	e := w.Writer.Close()
	if x := w.closer.Close(); e == nil {
		e = x
	}
	return e
}
func Encoding(name string) encoding.Encoding {
	switch strings.ToLower(name) {
	case "gbk":
		return simplifiedchinese.GBK
	case "gb18030":
		return simplifiedchinese.GB18030
	case "big5":
		return traditionalchinese.Big5
	case "shift_jis":
		return japanese.ShiftJIS
	}
	return nil
}
func dialTransport(ctx context.Context, address string, p *model.Proxy) (net.Conn, error) {
	d := &net.Dialer{}
	if p == nil {
		return d.DialContext(ctx, "tcp", address)
	}
	if e := p.Validate(); e != nil {
		return nil, e
	}
	pa := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	if p.Kind == "socks5" {
		var a *proxy.Auth
		if p.User != "" {
			a = &proxy.Auth{User: p.User, Password: p.Password}
		}
		pd, e := proxy.SOCKS5("tcp", pa, a, d)
		if e != nil {
			return nil, e
		}
		return pd.(proxy.ContextDialer).DialContext(ctx, "tcp", address)
	}
	conn, e := d.DialContext(ctx, "tcp", pa)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	if dl, has := ctx.Deadline(); has {
		conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	h, ps, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	port, e := strconv.Atoi(ps)
	if e != nil {
		return nil, e
	}
	if strings.ContainsRune(h, 0) || strings.ContainsRune(p.User, 0) {
		return nil, errors.New("SOCKS4 NUL input")
	}
	ip := net.ParseIP(h).To4()
	domain := false
	if ip == nil {
		ip = net.IPv4(0, 0, 0, 1).To4()
		domain = true
	}
	req := []byte{4, 1, byte(port >> 8), byte(port)}
	req = append(req, ip...)
	req = append(req, []byte(p.User)...)
	req = append(req, 0)
	if domain {
		req = append(req, []byte(h)...)
		req = append(req, 0)
	}
	if _, e = conn.Write(req); e != nil {
		return nil, e
	}
	reply := make([]byte, 8)
	if _, e = io.ReadFull(conn, reply); e != nil {
		return nil, e
	}
	if reply[0] != 0 || reply[1] != 90 {
		return nil, fmt.Errorf("SOCKS4 rejected: %d", reply[1])
	}
	conn.SetDeadline(time.Time{})
	ok = true
	return conn, nil
}
func GenerateKey() (private, public string, err error) { return GenerateKeyWithType("ed25519", "") }
func GenerateKeyWithType(algorithm, comment string) (private, public string, err error) {
	var key any
	switch algorithm {
	case "ed25519":
		_, k, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return "", "", e
		}
		key = k
	case "rsa-2048", "rsa-4096":
		bits := 2048
		if algorithm == "rsa-4096" {
			bits = 4096
		}
		k, e := rsa.GenerateKey(rand.Reader, bits)
		if e != nil {
			return "", "", e
		}
		key = k
	default:
		return "", "", errors.New("unsupported key algorithm")
	}
	signer, e := ssh.NewSignerFromKey(key)
	if e != nil {
		return "", "", e
	}
	block, e := ssh.MarshalPrivateKey(key, comment)
	if e != nil {
		return "", "", e
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if comment != "" {
		line += " " + comment
	}
	return string(pem.EncodeToMemory(block)), line + "\n", nil
}

func InspectKey(private, pass string) (string, error) {
	var s ssh.Signer
	var e error
	if pass == "" {
		s, e = ssh.ParsePrivateKey([]byte(private))
	} else {
		s, e = ssh.ParsePrivateKeyWithPassphrase([]byte(private), []byte(pass))
	}
	if e != nil {
		return "", e
	}
	return string(ssh.MarshalAuthorizedKey(s.PublicKey())), nil
}
