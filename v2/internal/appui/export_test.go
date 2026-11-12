package appui

// SetOS makes the shortcut labels read as written for another system ("darwin", "linux", "windows"), for
// tests. The returned func puts the real one back.
func SetOS(goos string) (restore func()) {
	old := osName
	osName = goos
	return func() { osName = old }
}
