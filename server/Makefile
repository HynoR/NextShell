# nshellserver 多平台生产构建
# 支持: darwin/linux/windows × amd64/arm64

BINARY   := nshellserver
MODULE   := github.com/hynor/nshellserver
DIST     := dist
LDFLAGS  := -s -w
GOFLAGS  := -trimpath

# 构建矩阵: OS_ARCH
PLATFORMS := darwin_amd64 darwin_arm64 linux_amd64 linux_arm64 windows_amd64 windows_arm64

.PHONY: all build clean $(PLATFORMS)

all: $(DIST) $(PLATFORMS)

# 当前平台本地构建（便于开发/调试）
build: $(DIST)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

# 各平台交叉编译
darwin_amd64: $(DIST)
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-amd64 .

darwin_arm64: $(DIST)
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-arm64 .

linux_amd64: $(DIST)
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-amd64 .

linux_arm64: $(DIST)
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-arm64 .

windows_amd64: $(DIST)
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-windows-amd64.exe .

windows_arm64: $(DIST)
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-windows-arm64.exe .

clean:
	rm -rf $(DIST)

$(DIST):
	mkdir -p $(DIST)
