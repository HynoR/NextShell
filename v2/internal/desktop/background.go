package desktop

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui"
)

// The picture behind the main window is kept as a copy in the data folder, so that moving or deleting the
// original changes nothing. Its name goes in the preferences, never a path.

const (
	backgroundDir       = "background" // under the data folder
	backgroundMaxBytes  = 20 << 20
	backgroundMaxPixels = 25_000_000 // a decoded picture takes 4 bytes a pixel
)

// SetDataDir sets the folder for the app's own files, which is the profile's folder unless the profile is in
// memory; without one no background picture can be kept.
func (p *Platform) SetDataDir(dir string) { p.mu.Lock(); p.dataDir = dir; p.mu.Unlock() }

func (p *Platform) backgroundFolder() string {
	p.mu.Lock()
	dir := p.dataDir
	p.mu.Unlock()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, backgroundDir)
}

// ImportBackground copies the picture at src into the data folder, under a name made of its content, and
// returns that name and the decoded picture. It refuses what is not a picture the window can draw, and what
// is too big: the copy is decoded in full, so a huge one would cost memory on every run. The messages are
// shown to the user as they are.
func (p *Platform) ImportBackground(src string) (string, *ui.Bitmap, error) {
	dir := p.backgroundFolder()
	if dir == "" {
		return "", nil, errors.New("无法设置背景图：没有可用的数据文件夹")
	}
	return importBackground(src, dir)
}

func importBackground(src, dir string) (string, *ui.Bitmap, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", nil, errors.New("无法打开这个文件")
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return "", nil, errors.New("这不是一个图片文件")
	}
	data, err := io.ReadAll(io.LimitReader(f, backgroundMaxBytes+1))
	if err != nil {
		return "", nil, errors.New("无法读取这个文件")
	}
	if len(data) > backgroundMaxBytes {
		return "", nil, fmt.Errorf("图片太大了（超过 %d MB），请换一张小一点的", backgroundMaxBytes>>20)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !slices.Contains([]string{"png", "jpeg", "gif", "webp", "bmp"}, format) {
		return "", nil, errors.New("这不是可用的图片，请选择 PNG、JPEG、GIF、WebP 或 BMP 图片")
	}
	if cfg.Width*cfg.Height > backgroundMaxPixels {
		return "", nil, fmt.Errorf("图片尺寸太大（%d×%d），请选择不超过 %d 万像素的图片", cfg.Width, cfg.Height, backgroundMaxPixels/10000)
	}
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		return "", nil, errors.New("图片已损坏，无法读取")
	}
	sum := sha256.Sum256(data)
	ext := format
	if ext == "jpeg" {
		ext = "jpg"
	}
	name := "background-" + hex.EncodeToString(sum[:6]) + "." + ext
	unwritable := errors.New("无法保存背景图：数据文件夹不可写")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, unwritable
	}
	tmp, err := os.CreateTemp(dir, ".import-*")
	if err != nil {
		return "", nil, unwritable
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(dir, name))
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", nil, errors.New("无法保存背景图：磁盘空间不足或不可写")
	}
	return name, bmp, nil
}

// BackgroundPicture reads the copy called name.
func (p *Platform) BackgroundPicture(name string) (*ui.Bitmap, error) {
	dir := p.backgroundFolder()
	if dir == "" || name != filepath.Base(name) {
		return nil, errors.New("没有可用的数据文件夹")
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	return ui.DecodeBitmap(data)
}

// RemoveBackground deletes the copy called name, which may already be gone.
func (p *Platform) RemoveBackground(name string) {
	if dir := p.backgroundFolder(); dir != "" && name != "" && name == filepath.Base(name) {
		os.Remove(filepath.Join(dir, name))
	}
}
