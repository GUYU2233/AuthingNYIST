package platform

import (
	"context"
	"fmt"
	"io"

	"github.com/Palvef/AuthingNYIST/internal/update"
	"github.com/Palvef/AuthingNYIST/internal/version"
)

var checkRelease = update.Check

func printUpdate(ctx context.Context, out io.Writer) error {
	result, err := checkRelease(ctx, version.Version)
	if err != nil {
		return fmt.Errorf("检查更新失败: %w", err)
	}
	fmt.Fprintf(out, "当前版本: %s\nGitHub 最新稳定版: %s\n", version.Version, result.Latest.TagName)
	switch {
	case !result.CurrentKnown:
		fmt.Fprintln(out, "当前为开发/自定义构建，无法判断是否需要更新。")
	case result.UpdateAvailable:
		fmt.Fprintln(out, "有新版本可用。请下载后核验校验和并手动替换；不会自动安装。")
	default:
		fmt.Fprintln(out, "无需更新（当前版本不低于最新稳定版）。")
	}
	fmt.Fprintln(out, result.Latest.HTMLURL)
	return nil
}
