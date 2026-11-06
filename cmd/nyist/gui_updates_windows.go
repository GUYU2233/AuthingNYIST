//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/update"
	"github.com/Palvef/AuthingNYIST/internal/version"
	"github.com/lxn/walk"
)

// startup never joins or switches Wi-Fi. Detection is advisory, not proof that
// the portal session is online, and must not overwrite edits made during probing.
func (d *desktop) startup() {
	cfg := d.cfg
	original := d.network.Text()
	generation := d.generation
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ctx, cancel := context.WithTimeout(d.lifetime, 20*time.Second)
		defer cancel()
		network, detectErr := app.DetectNetwork(ctx)
		inspected := cfg
		if network == "nyist-tec" || network == "nyist-stu" {
			inspected.Network = network
		}
		// Authoritative portal check, including other-account protection; never infer
		// authentication from SSID, routes, or Internet reachability.
		status, statusErr := app.Check(ctx, inspected)
		if ctx.Err() != nil {
			return
		}
		d.mw.Synchronize(func() {
			if d.exiting || d.busy || d.generation != generation {
				return
			}
			if d.network.Text() != original || d.username.Text() != cfg.Username || d.password.Text() != cfg.Password {
				return
			}
			if network == "nyist-tec" {
				d.network.SetCurrentIndex(0)
			}
			if network == "nyist-stu" {
				d.network.SetCurrentIndex(1)
			}
			if statusErr != nil {
				d.setStatus("启动状态检查失败，未自动认证: " + statusErr.Error())
				return
			}
			d.setStatus(statusText(status))
			if detectErr != nil {
				d.setStatus(statusText(status) + "；网络检测: " + detectErr.Error())
				return
			}
			// A different online account is never replaced, even with startup enabled.
			if status.Online || status.Message == "Another account is online" {
				return
			}
			if d.serviceStateKnown && !d.serviceEnabled && cfg.AutoConnect && cfg.Username != "" && cfg.Password != "" {
				// Auto mode re-verifies the current association and never switches
				// profiles if Wi-Fi changed while the startup check was running.
				inspected.Network = "auto"
				d.connectConfig(inspected)
			}
		})
	}()
}

func updateMessage(r *update.Result) string {
	if !r.CurrentKnown {
		return fmt.Sprintf("当前版本 %s（开发或未知版本）；最新稳定版 %s，无法自动判断升级。", r.CurrentVersion, r.Latest.TagName)
	}
	if r.UpdateAvailable {
		return fmt.Sprintf("发现新版本 %s（当前 %s）", r.Latest.TagName, r.CurrentVersion)
	}
	return fmt.Sprintf("当前版本 %s；最新稳定版 %s，无需更新。", r.CurrentVersion, r.Latest.TagName)
}

func standaloneUpdateCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, err := update.Check(ctx, version.Version)
	if err != nil {
		fatal(fmt.Errorf("检查更新失败: %w", err))
		return
	}
	walk.MsgBox(nil, "NYIST Connect · 更新", updateMessage(r)+"\n请打开主窗口使用已验证下载。", walk.MsgBoxIconInformation)
}

func (d *desktop) checkUpdates(manual bool) {
	if d.exiting || d.updateBusy {
		return
	}
	d.updateBusy = true
	d.updateButton.SetEnabled(false)
	d.updateAction.SetEnabled(false)
	d.updateStatus.SetText("正在检查更新 …")
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		r, err := update.Check(d.lifetime, version.Version)
		if d.lifetime.Err() != nil {
			return
		}
		d.mw.Synchronize(func() {
			if d.exiting {
				return
			}
			d.updateBusy = false
			d.updateButton.SetEnabled(true)
			d.updateAction.SetEnabled(true)
			if err != nil {
				d.updateStatus.SetText("检查更新失败，可稍后重试")
				if manual {
					walk.MsgBox(d.mw, "检查更新", err.Error(), walk.MsgBoxIconWarning)
				}
				return
			}
			d.updateStatus.SetText(updateMessage(r))
			if !r.UpdateAvailable {
				if manual {
					walk.MsgBox(d.mw, "检查更新", updateMessage(r), walk.MsgBoxIconInformation)
				}
				return
			}
			// Startup only announces availability; downloading always requires consent.
			if !manual {
				return
			}
			answer := walk.MsgBox(d.mw, "下载更新", updateMessage(r)+"\n下载并校验 Windows EXE？不会自动安装、替换正在运行的程序或停止服务。", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion)
			if answer == walk.DlgCmdYes && !d.exiting {
				d.downloadUpdate(r.Latest)
			}
		})
	}()
}

func (d *desktop) downloadUpdate(release update.Release) {
	if d.exiting || d.updateBusy {
		return
	}
	d.updateBusy = true
	d.updateButton.SetEnabled(false)
	d.updateAction.SetEnabled(false)
	d.updateStatus.SetText("正在下载并校验更新 …")
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		result, err := update.StageWindows(d.lifetime, release)
		if d.lifetime.Err() != nil {
			return
		}
		d.mw.Synchronize(func() {
			if d.exiting {
				return
			}
			d.updateBusy = false
			d.updateButton.SetEnabled(true)
			d.updateAction.SetEnabled(true)
			if err != nil {
				d.updateStatus.SetText("更新下载失败")
				walk.MsgBox(d.mw, "下载更新", err.Error(), walk.MsgBoxIconWarning)
				return
			}
			d.updateStatus.SetText("更新已校验，等待手动安装")
			message := "下载已完成并通过 SHA256 校验：\n" + result.Path + "\n\n请退出程序后手动替换原 EXE。开机认证服务需单独更新；本程序不会自动停止服务或安装更新。\n现在打开下载目录？"
			if walk.MsgBox(d.mw, "更新已准备就绪", message, walk.MsgBoxYesNo|walk.MsgBoxIconInformation) == walk.DlgCmdYes && !d.exiting {
				if err := exec.Command("explorer.exe", result.Directory).Start(); err != nil {
					d.updateStatus.SetText("无法打开目录: " + result.Directory)
				}
			}
		})
	}()
}
