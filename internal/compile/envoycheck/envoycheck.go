package envoycheck

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// EnvVarPath 是显式指定 envoy 二进制路径的环境变量（发现顺序第一优先，
// 见下发层 §5.6；CLI 的 --envoy-validate=<path> 优先于它）。
const EnvVarPath = "ESGW_ENVOY_PATH"

// DefaultTimeout 是单次 `envoy --mode validate` 的默认超时。
// validate 只做配置加载与校验，正常在秒级完成；超时防御二进制卡死。
const DefaultTimeout = 30 * time.Second

// stderrTail 是校验失败时带回的 envoy stderr 上限（排障关键信息在尾部）。
const stderrTail = 4096

// ErrBinaryNotFound 表示未发现 envoy 二进制（调用方按 F7 语义降级为 Warning）。
var ErrBinaryNotFound = fmt.Errorf("envoy binary not found")

// FindBinary 按优先级发现 envoy 二进制：显式路径（CLI flag 值）→
// ESGW_ENVOY_PATH 环境变量 → PATH 中的 "envoy"。返回可用路径；
// 全部落空时返回包装了 ErrBinaryNotFound 的错误。
func FindBinary(explicit string) (string, error) {
	if explicit != "" {
		if err := checkExecutable(explicit); err != nil {
			return "", fmt.Errorf("%w: %v", ErrBinaryNotFound, err)
		}
		return explicit, nil
	}
	if p := os.Getenv(EnvVarPath); p != "" {
		if err := checkExecutable(p); err != nil {
			return "", fmt.Errorf("%w: $%s=%q: %v", ErrBinaryNotFound, EnvVarPath, p, err)
		}
		return p, nil
	}
	p, err := exec.LookPath("envoy")
	if err != nil {
		return "", fmt.Errorf("%w: %q not in PATH (set $%s or use --envoy-validate=<path>)", ErrBinaryNotFound, "envoy", EnvVarPath)
	}
	return p, nil
}

// checkExecutable 验证路径存在、是常规文件且有可执行位。
func checkExecutable(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if st.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

// Validate 用 `envoy --mode validate -c <tmpfile>` 校验一份渲染好的 static
// 配置（F7，编译层 §3）。config 为完整 envoy.yaml 内容；实现写入临时文件
// （envoy 只接受文件路径），执行结束后清理。
//
// 失败时返回的错误含 envoy stderr 尾部（≤4KB）；超时被识别并明确报告。
// 调用方负责把失败包装为 Stage=envoy 的 CompileError（Error 级），
// 二进制缺失（FindBinary 失败）则包装为 Warning 级——本包只报告事实。
func Validate(ctx context.Context, binPath string, config []byte, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	f, err := os.CreateTemp("", "esgw-validate-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(config); err != nil {
		_ = f.Close()
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write temp config file: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "--mode", "validate", "-c", tmp)
	var out bytes.Buffer
	cmd.Stdout = &out // envoy 的校验结论也走 stderr；stdout 一并捕获便于排障
	cmd.Stderr = &out
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("envoy --mode validate timed out after %s", timeout)
	}
	if runErr != nil {
		return fmt.Errorf("envoy --mode validate failed: %v\n%s", runErr, tail(out.String(), stderrTail))
	}
	return nil
}

// tail 返回 s 的最后 n 字节（按行对齐，避免截断多字节字符行首）。
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := bytes.IndexByte([]byte(s), '\n'); i >= 0 && i+1 < len(s) {
		s = s[i+1:]
	}
	return "... (truncated)\n" + s
}
