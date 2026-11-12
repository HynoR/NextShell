//go:build interop

// Package interop runs the core packages (sshclient, sftpfs, transfer, monitor, remoteedit) against
// real OpenSSH servers in Docker. Everything here is behind the `interop` build tag, so a plain
// `go test ./...` never touches it. Dockerfiles live in v2/testdata/interop.
package interop

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/sshclient"
)

// Variant is one image: a Dockerfile directory plus build args.
type Variant struct {
	Name, Dir string
	Args      []string
}

var (
	Debian = Variant{Name: "debian", Dir: "debian"}                                      // Debian 13, OpenSSH 10.x, PAM, GNU userland
	Ubuntu = Variant{Name: "ubuntu", Dir: "debian", Args: []string{"BASE=ubuntu:22.04"}} // older OpenSSH 8.9
	Alpine = Variant{Name: "alpine", Dir: "alpine"}                                      // BusyBox, no PAM
	Socks  = Variant{Name: "socks5", Dir: "socks5"}                                      // Dante SOCKS5 proxy
	runID  = hex.EncodeToString(randBytes(4))
	seq    atomic.Int64
)

const (
	Password  = "test-password"
	Label     = "nextshell-interop"
	imageTag  = "1"
	buildRoot = "../../testdata/interop"
)

func randBytes(n int) []byte { b := make([]byte, n); rand.Read(b); return b }

var (
	dockerOnce sync.Once
	dockerErr  error
)

// RequireDocker skips t when the docker daemon is not reachable.
func RequireDocker(t testing.TB) {
	t.Helper()
	dockerOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, dockerErr = docker(ctx, nil, "info", "--format", "{{.ServerVersion}}")
	})
	if dockerErr != nil {
		t.Skipf("docker unavailable: %v", dockerErr)
	}
}

func docker(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = stdin
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	s := strings.TrimSpace(out.String())
	if err != nil {
		return s, fmt.Errorf("docker %s: %w: %s", strings.Join(args[:min(len(args), 2)], " "), err, s)
	}
	return s, nil
}

var (
	buildMu sync.Mutex
	built   = map[string]error{}
)

func image(t testing.TB, v Variant) string {
	t.Helper()
	tag := "nextshell-interop/" + v.Name + ":" + imageTag
	buildMu.Lock()
	defer buildMu.Unlock()
	err, ok := built[v.Name]
	if !ok {
		args := []string{"build", "-q", "-t", tag}
		for _, a := range v.Args {
			args = append(args, "--build-arg", a)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, err = docker(ctx, nil, append(args, buildRoot+"/"+v.Dir)...)
		built[v.Name] = err
	}
	if err != nil {
		t.Fatalf("build %s image: %v", v.Name, err)
	}
	return tag
}

// Keys are generated once per test process and installed in every sshd container.
var Keys struct {
	Ed25519, RSA, Encrypted model.Key
	Passphrase              string
}
var keysOnce sync.Once

func initKeys(t testing.TB) {
	keysOnce.Do(func() {
		must := func(e error) {
			if e != nil {
				panic(e)
			}
		}
		mk := func(id, alg string) model.Key {
			priv, pub, e := sshclient.GenerateKeyWithType(alg, id)
			must(e)
			return model.Key{ID: id, Name: id, Private: priv, Public: pub}
		}
		Keys.Ed25519 = mk("ed25519", "ed25519")
		Keys.RSA = mk("rsa", "rsa-2048")
		Keys.Passphrase = "correct horse battery staple"
		_, k, e := ed25519.GenerateKey(rand.Reader)
		must(e)
		block, e := ssh.MarshalPrivateKeyWithPassphrase(k, "encrypted", []byte(Keys.Passphrase))
		must(e)
		signer, e := ssh.NewSignerFromKey(k)
		must(e)
		Keys.Encrypted = model.Key{ID: "encrypted", Name: "encrypted", Private: string(pem.EncodeToMemory(block)), Passphrase: Keys.Passphrase, Public: string(ssh.MarshalAuthorizedKey(signer.PublicKey()))}
	})
}

func authorizedKeys() string {
	return Keys.Ed25519.Public + Keys.RSA.Public + Keys.Encrypted.Public
}

// Container is one running container; Ports maps container port -> published 127.0.0.1 port.
type Container struct {
	Name    string
	Variant Variant
	Ports   map[int]int
}

type Spec struct {
	Variant  Variant
	HostPort int    // fixed published sshd port (to recreate a server at the same address); 0 = random
	Network  string // docker network name, "" = default bridge
	Alias    string // network alias inside Network
	Ports    []int  // container ports to publish; default [22]
}

var (
	cleanMu    sync.Mutex
	cleanNets  []string
	sharedMu   sync.Mutex
	sharedBox  = map[string]*Container{}
	sharedFail = map[string]error{}
)

// Start launches a container and waits until its service answers. It is removed when t ends.
func Start(t testing.TB, s Spec) *Container {
	t.Helper()
	c, err := start(t, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Remove() })
	return c
}

func start(t testing.TB, s Spec) (*Container, error) {
	RequireDocker(t)
	initKeys(t)
	tag := image(t, s.Variant)
	c := &Container{Name: fmt.Sprintf("nsi-%s-%d", runID, seq.Add(1)), Variant: s.Variant, Ports: map[int]int{}}
	ports := s.Ports
	if len(ports) == 0 {
		ports = []int{22}
	}
	args := []string{"run", "-d", "--rm", "--name", c.Name, "--label", Label + "=" + runID, "-e", "AUTHORIZED_KEYS=" + authorizedKeys()}
	if s.Network != "" {
		args = append(args, "--network", s.Network)
		if s.Alias != "" {
			args = append(args, "--network-alias", s.Alias)
		}
	}
	for _, p := range ports {
		host := ""
		if p == 22 && s.HostPort != 0 {
			host = strconv.Itoa(s.HostPort)
		}
		args = append(args, "-p", "127.0.0.1:"+host+":"+strconv.Itoa(p))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := docker(ctx, nil, append(args, tag)...); err != nil {
		return nil, err
	}
	for _, p := range ports {
		out, err := docker(ctx, nil, "port", c.Name, strconv.Itoa(p)+"/tcp")
		if err != nil {
			c.Remove()
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			if h, port, e := net.SplitHostPort(strings.TrimSpace(line)); e == nil && h == "127.0.0.1" {
				c.Ports[p], _ = strconv.Atoi(port)
			}
		}
		if c.Ports[p] == 0 {
			c.Remove()
			return nil, fmt.Errorf("no published port for %d: %q", p, out)
		}
	}
	if err := c.waitReady(ports[0]); err != nil {
		logs, _ := docker(ctx, nil, "logs", "--tail", "30", c.Name)
		c.Remove()
		return nil, fmt.Errorf("%w\n%s", err, logs)
	}
	return c, nil
}

// waitReady speaks the real protocol: Docker's port forwarder accepts TCP before the service listens.
func (c *Container) waitReady(port int) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", c.Addr(port), time.Second)
		if err == nil {
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			if port == 1080 || port == 1081 {
				conn.Write([]byte{5, 1, 0, 2})
				b := make([]byte, 2)
				_, err = io.ReadFull(conn, b)
			} else {
				b := make([]byte, 4)
				_, err = io.ReadFull(conn, b)
				if err == nil && string(b) != "SSH-" {
					err = fmt.Errorf("unexpected banner %q", b)
				}
			}
			conn.Close()
			if err == nil {
				return nil
			}
		}
		last = err
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%s not ready on port %d: %v", c.Name, port, last)
}

func (c *Container) Addr(containerPort int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Ports[containerPort]))
}
func (c *Container) HostPort() int { return c.Ports[22] }

// Shared returns a per-process container for read-mostly tests; TestMain removes it.
func Shared(t testing.TB, v Variant) *Container {
	t.Helper()
	RequireDocker(t)
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if err := sharedFail[v.Name]; err != nil {
		t.Fatal(err)
	}
	if c := sharedBox[v.Name]; c != nil {
		return c
	}
	c, err := start(t, Spec{Variant: v})
	if err != nil {
		sharedFail[v.Name] = err
		t.Fatal(err)
	}
	sharedBox[v.Name] = c
	return c
}

// Network creates a user-defined docker network that is removed when t ends.
func Network(t testing.TB) string {
	t.Helper()
	RequireDocker(t)
	name := fmt.Sprintf("nsi-net-%s-%d", runID, seq.Add(1))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := docker(ctx, nil, "network", "create", "--label", Label+"="+runID, name); err != nil {
		t.Fatal(err)
	}
	cleanMu.Lock()
	cleanNets = append(cleanNets, name)
	cleanMu.Unlock()
	t.Cleanup(func() { docker(context.Background(), nil, "network", "rm", name) })
	return name
}

// Cleanup removes everything this process started; called from TestMain.
func Cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if ids, _ := docker(ctx, nil, "ps", "-aq", "--filter", "label="+Label+"="+runID); ids != "" {
		docker(ctx, nil, append([]string{"rm", "-f"}, strings.Fields(ids)...)...)
	}
	cleanMu.Lock()
	defer cleanMu.Unlock()
	for _, n := range cleanNets {
		docker(ctx, nil, "network", "rm", n)
	}
}

func (c *Container) Remove()  { docker(context.Background(), nil, "rm", "-f", c.Name) }
func (c *Container) Kill()    { docker(context.Background(), nil, "kill", c.Name) }
func (c *Container) Stop()    { docker(context.Background(), nil, "stop", "-t", "1", c.Name) }
func (c *Container) Pause()   { docker(context.Background(), nil, "pause", c.Name) }
func (c *Container) Unpause() { docker(context.Background(), nil, "unpause", c.Name) }

// Exec runs a shell script inside the container as root.
func (c *Container) Exec(t testing.TB, script string) string {
	t.Helper()
	out, err := c.TryExec(script)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func (c *Container) TryExec(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return docker(ctx, nil, "exec", c.Name, "sh", "-c", script)
}

// HostFingerprints lists the SHA256 fingerprints of every host key the container offers.
func (c *Container) HostFingerprints(t testing.TB) []string {
	t.Helper()
	var fps []string
	for _, line := range strings.Split(c.Exec(t, `for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf $f; done`), "\n") {
		if f := strings.Fields(line); len(f) >= 2 {
			fps = append(fps, f[1])
		}
	}
	return fps
}

// Options returns password auth as user "tester", trusting any host key (first-use accepted).
func (c *Container) Options() sshclient.Options {
	return sshclient.Options{
		Timeout:    20 * time.Second,
		Connection: model.Connection{Name: c.Name, Host: "127.0.0.1", Port: c.HostPort(), User: "tester", Auth: "password", Secret: Password, Encoding: "utf-8"},
		HostKey:    func(string, string) error { return nil },
	}
}

// KeyOptions authenticates with k.
func (c *Container) KeyOptions(k model.Key) sshclient.Options {
	o := c.Options()
	o.Connection.Auth, o.Connection.KeyID, o.Connection.Secret = "key", k.ID, ""
	o.Key = k
	return o
}

// Dial connects with o and closes the session when t ends.
func Dial(t testing.TB, o sshclient.Options) *sshclient.Session {
	t.Helper()
	s, err := sshclient.Dial(context.Background(), o)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Sh runs one command on an exec channel (never the interactive PTY) and returns combined output.
func Sh(t testing.TB, s *sshclient.Session, command string) string {
	t.Helper()
	out, err := TrySh(s, command, 60*time.Second)
	if err != nil {
		t.Fatalf("%s: %v\n%s", command, err, out)
	}
	return out
}
func TrySh(s *sshclient.Session, command string, limit time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	b, err := s.Output(ctx, command, 64<<20)
	return string(b), err
}

// Term drives an interactive PTY session without a terminal emulator: it buffers everything the
// server sends and lets a test wait for a substring.
type Term struct {
	S   *sshclient.Session
	mu  sync.Mutex
	buf []byte
	err error
}

func NewTerm(t testing.TB, s *sshclient.Session) *Term {
	x := &Term{S: s}
	go func() {
		b := make([]byte, 32<<10)
		for {
			n, err := s.Read(b)
			x.mu.Lock()
			x.buf = append(x.buf, b[:n]...)
			if err != nil {
				x.err = err
				x.mu.Unlock()
				return
			}
			x.mu.Unlock()
		}
	}()
	return x
}

func (x *Term) Send(s string) {
	if _, err := x.S.Write([]byte(s)); err != nil {
		x.mu.Lock()
		x.err = errors.Join(x.err, err)
		x.mu.Unlock()
	}
}

// Expect waits for substr and returns everything received before it (that text is consumed too).
func (x *Term) Expect(t testing.TB, substr string, d time.Duration) string {
	t.Helper()
	got, ok := x.WaitFor(substr, d)
	if !ok {
		x.mu.Lock()
		defer x.mu.Unlock()
		t.Fatalf("timeout after %v waiting for %q (err=%v); last output: %q", d, substr, x.err, tail(x.buf, 600))
	}
	return got
}
func (x *Term) WaitFor(substr string, d time.Duration) (string, bool) {
	deadline := time.Now().Add(d)
	for {
		x.mu.Lock()
		if i := bytes.Index(x.buf, []byte(substr)); i >= 0 {
			before := string(x.buf[:i])
			x.buf = x.buf[i+len(substr):]
			x.mu.Unlock()
			return before, true
		}
		dead := x.err != nil
		x.mu.Unlock()
		if dead || time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Sync proves the remote shell is processing input again: it echoes a number computed remotely,
// so the echoed command line itself cannot satisfy the match.
func (x *Term) Sync(t testing.TB, d time.Duration) {
	t.Helper()
	n := 1000 + int(seq.Add(1))
	x.Send(fmt.Sprintf("echo SYNC$((%d+1))\n", n-1))
	x.Expect(t, fmt.Sprintf("SYNC%d", n), d)
}
func (x *Term) Err() error { x.mu.Lock(); defer x.mu.Unlock(); return x.err }
func (x *Term) Output() string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return string(x.buf)
}

func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}
