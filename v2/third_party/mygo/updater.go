//go:build !js

package mygo

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/internal/update"
	"github.com/egoist/mygo/netproxy"
)

// Set by `mygo build` with -ldflags -X when mygo.config.ts configures updates:
// the manifest of this build's target, or with a tag prefix on GitHub its
// feed in the newest tagged release (update.TaggedFeed), and the public key
// updates must be signed with.
var (
	packageUpdateFeed string
	packageUpdateKey  string
)

// startExe is the executable the app started from. On Linux, os.Executable
// reports the old file once an update replaced it.
var startExe, _ = os.Executable()

// UpdaterModule installs new versions of the app, which `mygo build`
// publishes when mygo.config.ts configures updates: signed archives and a
// manifest per platform. Use the Updater singleton:
//
//	update, err := mygo.Updater.Check(ctx)
//	if err == nil && update != nil {
//		if err := update.Install(ctx, nil); err == nil {
//			mygo.App.Relaunch()
//		}
//	}
type UpdaterModule struct{}

// Updater installs new versions of the app.
var Updater = &UpdaterModule{}

// prereleases is set while Check offers prereleases (SetPrereleases).
var prereleases atomic.Bool

// updateSource is the source of SetSource, nil for the feed of
// mygo.config.ts.
var updateSource atomic.Pointer[UpdateSource]

// UpdateSource is where Updater gets updates from instead of the feed that
// mygo.config.ts configures (SetSource): an update service the app already
// has, an endpoint that needs the user's credentials, or a channel the app
// chooses while it runs. It serves what `mygo build` publishes, from
// wherever the app keeps it, and Updater checks it as it checks the feed's:
// the archive and deltas must be signed with the key of updates.publicKey
// that the app was built with, or they are not installed.
type UpdateSource interface {
	// Manifest returns the manifest of the running platform: the JSON that
	// `mygo build` writes as update-<target>.json, such as
	// update-darwin-arm64.json, of at most 1 MiB.
	// prereleases reports whether the app offers them (SetPrereleases).
	Manifest(ctx context.Context, prereleases bool) (io.ReadCloser, error)
	// Open returns the file at url, which the manifest names: the archive
	// of the update or one of its deltas. The URL is the manifest's, so it
	// may be anything the source understands, such as an id of its own.
	Open(ctx context.Context, url string) (io.ReadCloser, error)
}

// SetSource makes Check read the manifest from s, and Install download the
// files it names through s, instead of from the feed of mygo.config.ts; nil
// restores the feed. Updates are still enabled only in builds with updates
// in mygo.config.ts (Enabled), which give the key that updates are checked
// with. An Update that Check returned keeps downloading from the source it
// came from. It may be called from any goroutine.
func (u *UpdaterModule) SetSource(s UpdateSource) {
	if s == nil {
		updateSource.Store(nil)
		return
	}
	updateSource.Store(&s)
}

// ErrUpdatesDisabled is returned by Check for builds without updates:
// development builds, and builds of apps without updates in mygo.config.ts.
var ErrUpdatesDisabled = errors.New("mygo: updates are not enabled for this build")

// Update is a newer version of the app, found by Updater.Check.
type Update struct {
	// Version of the update, and its release notes in Markdown.
	Version string
	Notes   string
	// Date is when the update was built.
	Date time.Time

	manifest update.Manifest
	key      string
	// source is where the manifest came from, which serves its files.
	source UpdateSource
	// current is the version that Check compared with, which delta updates
	// update.
	current string
}

// Enabled reports whether the app can update itself: it was built with
// updates and can write where it is installed. Apps installed by a package
// manager, such as a .deb in /opt, are updated by it instead.
func (u *UpdaterModule) Enabled() bool {
	if packageUpdateFeed == "" || packageUpdateKey == "" || IsDev() {
		return false
	}
	target, err := installTarget()
	if err != nil {
		return false
	}
	probe, err := os.MkdirTemp(filepath.Dir(target), ".mygo-update-")
	if err != nil {
		return false
	}
	os.Remove(probe)
	return true
}

// Check asks the update feed, or the source of SetSource, for a version
// newer than the running one and returns it, or nil when the app is up to
// date.
func (u *UpdaterModule) Check(ctx context.Context) (*Update, error) {
	if !u.Enabled() {
		return nil, ErrUpdatesDisabled
	}
	var src UpdateSource = feedSource(packageUpdateFeed)
	if s := updateSource.Load(); s != nil {
		src = *s
	}
	return checkUpdate(ctx, src, packageUpdateKey, App.Version(), prereleases.Load())
}

// SetPrereleases makes Check offer prereleases too, such as 1.3.0-beta.1,
// for the users of an app who chose its beta: with updates on GitHub,
// Check then reads the release of the highest version that is not a
// draft, through the GitHub API (usually one request a check), so that a
// release newer than the prereleases is offered too. Check never offers a
// version older than the running one, so turning it off waits for the
// release after the prerelease the app runs. Updates of updates.url are
// the manifest the server has, whichever is set. It is off at launch; the
// app keeps the user's choice and sets it again. It may be called from
// any goroutine.
func (u *UpdaterModule) SetPrereleases(on bool) { prereleases.Store(on) }

// Prereleases reports whether Check offers prereleases (SetPrereleases).
func (u *UpdaterModule) Prereleases() bool { return prereleases.Load() }

// feedSource is the update feed that mygo.config.ts configures, the URL of
// a manifest or a feed that update.ResolveFeed resolves.
type feedSource string

func (f feedSource) Manifest(ctx context.Context, pre bool) (io.ReadCloser, error) {
	resolve := update.ResolveFeed
	if pre {
		resolve = update.ResolvePrereleaseFeed
	}
	feed, err := resolve(ctx, string(f), httpGet)
	if err != nil {
		return nil, err
	}
	return httpGet(ctx, feed)
}

func (f feedSource) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	return httpGet(ctx, url)
}

func checkUpdate(ctx context.Context, src UpdateSource, key, current string, pre bool) (*Update, error) {
	body, err := src.Manifest(ctx, pre)
	if err != nil {
		return nil, fmt.Errorf("mygo: checking for updates: %w", err)
	}
	defer body.Close()
	var m update.Manifest
	if err := json.UnmarshalRead(io.LimitReader(body, update.MaxManifestSize), &m); err != nil {
		return nil, fmt.Errorf("mygo: checking for updates: %w", err)
	}
	if m.Version == "" || m.URL == "" || m.Signature == "" {
		return nil, errors.New("mygo: checking for updates: incomplete manifest")
	}
	if update.Compare(m.Version, current) <= 0 {
		return nil, nil
	}
	up := &Update{Version: m.Version, Notes: m.Notes, manifest: m, key: key, current: current, source: src}
	up.Date, _ = time.Parse(time.RFC3339, m.Date)
	return up, nil
}

// Install downloads the update, checks that it is signed with the app's
// key and replaces the app with it; progress, when not nil, is called with
// the bytes downloaded so far. When the update has a delta for the running
// version, only the delta is downloaded, unless it fails to make the new
// version: the whole update is downloaded then, and the progress starts
// over. The running app is not affected: the new version runs after
// App.Relaunch, or at the next launch.
//
// The app must be able to write where it is installed: a bundle in
// /Applications of an administrator, or an app directory the user owns on
// Linux and Windows. Apps installed by a package manager are updated by it.
func (up *Update) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	target, err := installTarget()
	if err != nil {
		return err
	}
	get := httpGet
	if up.source != nil {
		get = up.source.Open
	}
	if err := installUpdate(ctx, get, up.manifest, up.key, up.current, target, progress); err != nil {
		return fmt.Errorf("mygo: installing the update: %w", err)
	}
	if runtime.GOOS == "linux" {
		// install.sh registered the desktop entry of the version it installed.
		if err := update.RefreshDesktopEntry(target, filepath.Base(startExe)); err != nil {
			log.Printf("mygo: registering the desktop entry of the update: %v", err)
		}
	}
	return nil
}

// installTarget returns what an update replaces: the app bundle on macOS,
// the directory of the executable elsewhere.
func installTarget() (string, error) {
	exe := startExe
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if runtime.GOOS == "darwin" {
		bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if !strings.HasSuffix(bundle, ".app") {
			return "", errors.New("mygo: only app bundles can update themselves")
		}
		return bundle, nil
	}
	if os.Getenv("APPIMAGE") != "" {
		return "", errors.New("mygo: AppImages cannot update themselves")
	}
	return filepath.Dir(exe), nil
}

// installUpdate installs the version of m in place of target, the app of
// version current: from its delta for current when there is one, else, or
// when the delta fails, from its archive, both downloaded with get.
func installUpdate(ctx context.Context, get update.Getter, m update.Manifest, key, current, target string, progress func(downloaded, total int64)) error {
	pub, err := update.ParsePublicKey(key)
	if err != nil {
		return err
	}
	// Next to the app, so that it can be renamed into place.
	work, err := os.MkdirTemp(filepath.Dir(target), "."+filepath.Base(target)+".update-")
	if err != nil {
		return fmt.Errorf("cannot write next to the app: %w", err)
	}
	staged := false
	defer func() {
		if !staged {
			os.RemoveAll(work)
		}
	}()
	unpacked := filepath.Join(work, "app")
	swap := swapFiles
	if runtime.GOOS == "darwin" {
		swap = func(dir, target string) error {
			err := stageBundle(work, dir, target)
			staged = err == nil
			return err
		}
	}

	if d := m.Delta(current); d != nil {
		err := applyDelta(ctx, get, *d, pub, m.Version, target, work, unpacked, progress)
		if err == nil {
			return swap(unpacked, target)
		}
		if ctx.Err() != nil {
			return err
		}
		log.Printf("mygo: downloading the whole update, as its delta update failed: %v", err)
		if err := os.RemoveAll(unpacked); err != nil {
			return err
		}
	}

	if m.Size <= 0 || m.Size > update.MaxArchiveSize {
		return fmt.Errorf("invalid size %d", m.Size)
	}
	archive := filepath.Join(work, "update.tar.gz")
	if err := download(ctx, get, m.URL, m.Size, m.Signature, pub, archive, progress); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	err = update.ExtractArchive(f, unpacked)
	f.Close()
	if err != nil {
		return err
	}
	return swap(unpacked, target)
}

// applyDelta downloads the delta d into work and makes the new version of
// the app at target from it, in dir: its bundle on macOS, in dir too, as
// the bundle of an archive is.
func applyDelta(ctx context.Context, get update.Getter, d update.Delta, pub ed25519.PublicKey, version, target, work, dir string, progress func(downloaded, total int64)) error {
	if d.Size <= 0 || d.Size > update.MaxArchiveSize {
		return fmt.Errorf("invalid size %d", d.Size)
	}
	path := filepath.Join(work, "update.delta")
	if err := download(ctx, get, d.URL, d.Size, d.Signature, pub, path, progress); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if runtime.GOOS == "darwin" {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return err
		}
		dir = filepath.Join(dir, filepath.Base(target))
	}
	return update.ApplyDelta(f, d.Size, d.From, version, target, dir)
}

// download fetches url with get into the file path and checks its size and
// signature.
func download(ctx context.Context, get update.Getter, url string, size int64, signature string, pub ed25519.PublicKey, path string, progress func(downloaded, total int64)) error {
	body, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), &progressReader{r: io.LimitReader(body, size+1), total: size, fn: progress})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	if n != size {
		return fmt.Errorf("downloaded %d bytes, want %d", n, size)
	}
	return update.Verify(pub, sum.Sum(nil), signature)
}

// stagedUpdate is a new version of the app bundle target, at bundle in
// work, a directory next to target that the update was made in, and the
// helper process that installs it once the app has exited.
type stagedUpdate struct {
	work, bundle, target string
	helper               *exec.Cmd
}

// pendingUpdate is the update that Install staged on macOS. It replaces the
// app's bundle once the app has exited, however it exits: replacing it
// while the app runs moved the bundle of the running app away, the Dock's
// tile of the app followed it, and the new version, opened where the app
// was, got a second tile.
var pendingUpdate atomic.Pointer[stagedUpdate]

// appPid is the process whose exit a staged update waits for: the app's,
// which tests replace.
var appPid = os.Getpid()

// stageBundle leaves the bundle that dir holds to replace target once the
// app has exited, in place of an update staged before.
func stageBundle(work, dir, target string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".app") {
		return errors.New("the update holds no app bundle")
	}
	p := &stagedUpdate{work: work, bundle: filepath.Join(dir, entries[0].Name()), target: target}
	args := []string{"-c", installAfterExit, "mygo-update", strconv.Itoa(appPid), p.target, p.bundle, p.work, p.target}
	if len(os.Args) > 1 {
		args = append(append(args, "--args"), os.Args[1:]...)
	}
	p.helper = exec.Command("/bin/sh", args...)
	if err := p.helper.Start(); err != nil {
		return err
	}
	if old := pendingUpdate.Swap(p); old != nil {
		_ = old.helper.Process.Kill()
		_ = old.helper.Wait()
		_ = os.RemoveAll(old.work)
	}
	return nil
}

// installAfterExit is the script of /bin/sh that installs a staged update:
// with the pid of the app, the target, bundle and work of the update and
// then the arguments of open, it waits for the app to exit, swaps the
// bundles, putting the old one back if the new one cannot be moved in, and
// opens the app again when Relaunch asked for it (relaunchUpdated). Through
// Launch Services, as openBundle, but without -n: the app has exited, and
// the Dock gives the new version the app's tile.
const installAfterExit = `pid=$1 target=$2 new=$3 work=$4
shift 4
trap '' HUP
while kill -0 "$pid" 2>/dev/null; do sleep 0.05; done
if mv "$target" "$work/old"; then
	mv "$new" "$target" || mv "$work/old" "$target"
fi
relaunch=
if [ -e "$work/relaunch" ]; then relaunch=1; fi
rm -rf "$work"
if [ -n "$relaunch" ]; then exec /usr/bin/open "$@"; fi
`

// relaunchUpdated asks the helper of a staged update to open the app once
// it has installed the update, reporting whether there is one.
func relaunchUpdated() bool {
	p := pendingUpdate.Load()
	return p != nil && os.WriteFile(filepath.Join(p.work, "relaunch"), nil, 0o644) == nil
}

// swapFiles replaces the entries of the app directory target with those in
// dir, and removes those that the version it replaces installed and dir
// lacks, which target's update.FileList names. Entries that no list names
// stay, as the data of a portable copy, and so do all the entries of an
// app installed before versions had a list: they cannot be told from the
// user's. Replaced and removed entries are renamed aside, and deleted once
// every entry of dir is in place; if one cannot be, dir's go back and the
// others are put back. The running executable is renamed away, which
// Windows allows, and removed at the next launch (cleanUpdateLeftovers).
// dir gets the list of its entries, which moves into target with them.
func swapFiles(dir, target string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	ours := map[string]bool{}
	for _, e := range entries {
		if e.Name() != update.FileList {
			names = append(names, e.Name())
			ours[e.Name()] = true
		}
	}
	if len(names) == 0 {
		return errors.New("the update is empty")
	}
	suffix := ".old-" + strconv.Itoa(os.Getpid())
	var moved, placed [][2]string // renamed: old name, new name
	undo := func() {
		for i := len(placed) - 1; i >= 0; i-- {
			if rename(placed[i][1], placed[i][0]) != nil {
				_ = os.RemoveAll(placed[i][1])
			}
		}
		for i := len(moved) - 1; i >= 0; i-- {
			_ = rename(moved[i][1], moved[i][0])
		}
	}
	moveAside := func(dst string) error {
		aside := filepath.Join(target, "."+filepath.Base(dst)+suffix)
		if err := rename(dst, aside); err != nil {
			return err
		}
		moved = append(moved, [2]string{dst, aside})
		return nil
	}

	// The entries that the replaced version installed and dir lacks go
	// first, so that one that dir has under another case, which is the same
	// entry on Windows and macOS, is moved aside before dir's takes its
	// place. The executable the app runs from stays, whatever the new
	// version calls its own: shortcuts, the desktop entry and Relaunch start
	// it. One that cannot be moved stays listed, for the next update to
	// remove.
	running := runningEntry(target)
	var left []string
	for _, name := range installedEntries(target) {
		dst := filepath.Join(target, name)
		if ours[name] || name == running {
			continue
		}
		if _, err := os.Lstat(dst); err != nil {
			continue
		}
		if err := moveAside(dst); err != nil {
			log.Printf("mygo: leaving %s, which the update no longer has: %v", dst, err)
			left = append(left, name)
		}
	}
	if err := update.WriteFileList(dir, append(names, left...)); err != nil {
		undo()
		return err
	}

	for _, name := range append(names, update.FileList) {
		src, dst := filepath.Join(dir, name), filepath.Join(target, name)
		if _, err := os.Lstat(dst); err == nil {
			if err := moveAside(dst); err != nil {
				undo()
				return err
			}
		}
		if err := rename(src, dst); err != nil {
			undo()
			return err
		}
		placed = append(placed, [2]string{src, dst})
	}
	for _, m := range moved {
		_ = os.RemoveAll(m[1])
	}
	return nil
}

// installedEntries returns the entries of the app directory target that
// its version installed, which its update.FileList names: none without
// one.
func installedEntries(target string) []string {
	b, err := readAppFile(filepath.Join(target, update.FileList))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("mygo: removing none of the files that the update no longer has: %v", err)
		}
		return nil
	}
	return update.ParseFileList(b)
}

// runningEntry returns the entry of the app directory target that the app
// runs from, or "" when it runs from elsewhere.
func runningEntry(target string) string {
	if startExe == "" {
		return ""
	}
	exe, dir := startExe, target
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if filepath.Dir(exe) != filepath.Clean(dir) {
		return ""
	}
	return filepath.Base(exe)
}

// relaunchNow starts the app again; it runs after the quit sequence.
func relaunchNow() {
	if launchedByDev() {
		os.Exit(devRelaunchCode)
	}
	if runtime.GOOS == "darwin" && (relaunchUpdated() || openBundle()) {
		return
	}
	// The executable the app started from, which an update may have
	// replaced.
	cmd := exec.Command(startExe, os.Args[1:]...)
	cmd.Dir = startDir
	if runtime.GOOS != "windows" {
		// GUI executables on Windows have no standard streams to pass on.
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// openBundle starts the app bundle again through Launch Services, as the
// Dock does, reporting whether it did. A child of this process would stay
// in this instance's coalition, which has no app once this instance exits:
// when an app that the new instance started quits, as one a terminal's
// shell ran, loginwindow takes the coalition's other processes for that
// app's and ends them with SIGTERM.
func openBundle() bool {
	bundle, err := installTarget()
	if err != nil {
		return false
	}
	// -n: this instance still runs, and would only be brought forward.
	args := []string{"-n", bundle}
	if len(os.Args) > 1 {
		args = append(append(args, "--args"), os.Args[1:]...)
	}
	return exec.Command("/usr/bin/open", args...).Run() == nil
}

// cleanUpdateLeftovers removes what an update could not remove while the
// previous version ran (Windows keeps running executables).
func cleanUpdateLeftovers() {
	if runtime.GOOS == "darwin" || startExe == "" {
		return
	}
	olds, _ := filepath.Glob(filepath.Join(filepath.Dir(startExe), ".*.old-*"))
	for _, p := range olds {
		_ = os.RemoveAll(p)
	}
}

// httpGet fetches a URL over HTTPS (or plain HTTP from the loopback
// interface, for tests), through the system's proxy (package netproxy).
func httpGet(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("refusing to download %s: updates need HTTPS", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// Not the app's name: GitHub's release assets come from a store that
	// answers 400 to a User-Agent with bytes outside ASCII, as 云音乐, for
	// every file its CDN has not cached yet.
	req.Header.Set("User-Agent", "mygo-updater")
	resp, err := (&http.Client{Transport: netproxy.Transport()}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return resp.Body, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// progressReader reports how much of a download was read.
type progressReader struct {
	r     io.Reader
	n     int64
	total int64
	fn    func(downloaded, total int64)
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if p.fn != nil && (err != nil || p.n == p.total || time.Since(p.last) > 100*time.Millisecond) {
		p.last = time.Now()
		p.fn(p.n, p.total)
	}
	return n, err
}
