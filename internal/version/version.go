// version.go 提供了用于构建版本信息的变量。
package version

// Version 是构建的版本号。
// Commit 是构建的Git提交哈希。
// BuildTime 是构建的时间戳。
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)
