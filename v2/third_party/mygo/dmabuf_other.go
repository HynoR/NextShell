//go:build !(linux && (amd64 || arm64))

package mygo

func linkDMABUFProbe() {}
