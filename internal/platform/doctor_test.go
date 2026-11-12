package platform

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/Palvef/AuthingNYIST/internal/app"
)

func fakeDoctorOptions() doctorOptions {
	opts := newDoctorOptions("/home/student/.config/AuthingNYIST/config.json")
	opts.stat = func(string) (os.FileMode, error) { return 0600, nil }
	opts.readFile = func(string) ([]byte, error) { return []byte(`{"password":"s3cret"}`), nil }
	opts.loadConfig = func(string) (app.Config, error) {
		cfg := app.DefaultConfig()
		cfg.Username = "student"
		cfg.Password = "s3cret"
		return cfg, nil
	}
	opts.detectNet = func(context.Context) (string, error) { return "nyist-tec", nil }
	opts.getenv = func(string) string { return "" }
	opts.dialContext = func(context.Context, string, string) (net.Conn, error) { return nil, nil }
	opts.defaultRoute = func(context.Context) (doctorRoute, error) {
		return doctorRoute{Interface: "wlan0", HardwareAddr: "aa:bb:cc:dd:ee:ff"}, nil
	}
	opts.resolver = func(context.Context) (doctorDNS, error) {
		return doctorDNS{Path: "/etc/resolv.conf", Exists: true, Nameserver: "10.0.0.1"}, nil
	}
	opts.keepalive = func(context.Context) (string, error) { return "已启用（systemd --user）", nil }
	opts.version = "v1.2.3"
	return opts
}

func TestDoctorReportTable(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*doctorOptions)
		wantErr    error
		contains   []string
		notContain []string
	}{
		{
			name:   "healthy",
			mutate: func(*doctorOptions) {},
			contains: []string{
				"配置文件: /home/student/.config/AuthingNYIST/config.json",
				"配置存在: 是",
				"配置可读: 是",
				"配置权限: 0600",
				"密码: 已设置",
				"网络选择: nyist-tec",
				"当前校园网络: nyist-tec",
				"网络一致性: 一致",
				"默认路由接口: wlan0（硬件地址 aa:bb:cc:dd:ee:ff）",
				"代理环境变量: 未设置（按设计忽略）",
				"DNS 配置: /etc/resolv.conf 存在（nameserver 10.0.0.1）",
				"门户 TCP 可达性: 可达（仅提示，不代表已联网或认证成功）",
				"版本: v1.2.3",
				"保活服务: 已启用（systemd --user）",
				"结论: 通过",
			},
			notContain: []string{"s3cret"},
		},
		{
			name: "missing config is blocking",
			mutate: func(o *doctorOptions) {
				o.stat = func(string) (os.FileMode, error) { return 0, os.ErrNotExist }
			},
			wantErr: errDoctorBlocking,
			contains: []string{
				"配置存在: 否（尚未配置账号）",
				"配置可读: 否（文件不存在）",
				"密码: 无法确认（配置不可用）",
				"结论: 未通过（2 个阻断项，0 个警告）",
			},
		},
		{
			name: "invalid config is blocking",
			mutate: func(o *doctorOptions) {
				o.loadConfig = func(string) (app.Config, error) {
					return app.DefaultConfig(), errors.New("invalid configuration JSON")
				}
			},
			wantErr:  errDoctorBlocking,
			contains: []string{"配置内容: 无效（invalid configuration JSON）", "结论: 未通过（1 个阻断项，0 个警告）"},
		},
		{
			name: "empty password is blocking",
			mutate: func(o *doctorOptions) {
				o.loadConfig = func(string) (app.Config, error) {
					cfg := app.DefaultConfig()
					cfg.Username = "student"
					return cfg, nil
				}
			},
			wantErr:  errDoctorBlocking,
			contains: []string{"密码: 未设置", "结论: 未通过（1 个阻断项，0 个警告）"},
		},
		{
			name: "permissive mode warns only",
			mutate: func(o *doctorOptions) {
				o.stat = func(string) (os.FileMode, error) { return 0644, nil }
			},
			contains: []string{"配置权限: 0644（警告：组或其他用户有权限）", "结论: 通过（1 个警告）"},
		},
		{
			name: "network mismatch warns only",
			mutate: func(o *doctorOptions) {
				o.detectNet = func(context.Context) (string, error) { return "nyist-stu", nil }
			},
			contains: []string{"当前校园网络: nyist-stu", "网络一致性: 不一致（当前 nyist-stu，配置为 nyist-tec）", "结论: 通过（1 个警告）"},
		},
		{
			name: "automatic selection with wired link",
			mutate: func(o *doctorOptions) {
				o.loadConfig = func(string) (app.Config, error) {
					cfg := app.DefaultConfig()
					cfg.Network = "auto"
					cfg.Password = "s3cret"
					return cfg, nil
				}
				o.detectNet = func(context.Context) (string, error) { return "", nil }
			},
			contains: []string{"网络选择: auto", "当前校园网络: 未检测到校园 Wi-Fi（可能使用有线网络）", "网络一致性: 自动（未检测到校园 Wi-Fi）"},
		},
		{
			name: "proxy names only never values",
			mutate: func(o *doctorOptions) {
				o.getenv = func(name string) string {
					if name == "HTTPS_PROXY" {
						return "http://alice:hunter2@proxy.example:8080"
					}
					return ""
				}
			},
			contains:   []string{"代理环境变量: HTTPS_PROXY 已设置（按设计忽略，程序不使用）"},
			notContain: []string{"alice", "hunter2", "proxy.example"},
		},
		{
			name: "portal unreachable is only a hint",
			mutate: func(o *doctorOptions) {
				o.dialContext = func(context.Context, string, string) (net.Conn, error) {
					return nil, errors.New("i/o timeout")
				}
			},
			contains: []string{"门户 TCP 可达性: 不可达（i/o timeout；仅提示，TUN 可能本地接受连接）", "结论: 通过"},
		},
		{
			name: "missing route and DNS are informational",
			mutate: func(o *doctorOptions) {
				o.defaultRoute = func(context.Context) (doctorRoute, error) { return doctorRoute{}, errors.New("未找到默认路由") }
				o.resolver = func(context.Context) (doctorDNS, error) {
					return doctorDNS{Path: "/etc/resolv.conf", Exists: false}, nil
				}
			},
			contains: []string{"默认路由接口: 未找到（未找到默认路由）", "DNS 配置: 未发现 /etc/resolv.conf", "结论: 通过"},
		},
		{
			name: "tun interface has no hardware address",
			mutate: func(o *doctorOptions) {
				o.defaultRoute = func(context.Context) (doctorRoute, error) {
					return doctorRoute{Interface: "tun0"}, nil
				}
			},
			contains: []string{"默认路由接口: tun0（无硬件地址，可能是 TUN/虚拟网卡）"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := fakeDoctorOptions()
			tt.mutate(&opts)
			var out bytes.Buffer
			err := runDoctor(context.Background(), &out, opts)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v\n%s", err, tt.wantErr, out.String())
			}
			text := out.String()
			for _, want := range tt.contains {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in output:\n%s", want, text)
				}
			}
			for _, forbidden := range tt.notContain {
				if strings.Contains(text, forbidden) {
					t.Errorf("output leaked %q:\n%s", forbidden, text)
				}
			}
			// Every report line must be a "key: value" pair.
			for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
				if !strings.Contains(line, ": ") {
					t.Errorf("line is not key: value: %q", line)
				}
			}
		})
	}
}

func TestPortalAddress(t *testing.T) {
	tests := []struct {
		host, want string
		wantErr    bool
	}{
		{"https://auth.nyist.edu.cn", "auth.nyist.edu.cn:443", false},
		{"http://auth.nyist.edu.cn", "auth.nyist.edu.cn:80", false},
		{"auth.nyist.edu.cn", "auth.nyist.edu.cn:443", false},
		{"https://auth.nyist.edu.cn:8443/", "auth.nyist.edu.cn:8443", false},
		{"", "auth.nyist.edu.cn:443", false},
		{"https://user:pass@auth.nyist.edu.cn", "", true},
		{"ftp://auth.nyist.edu.cn", "", true},
	}
	for _, tt := range tests {
		got, err := portalAddress(tt.host)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("portalAddress(%q) = %q, %v; want %q, err=%v", tt.host, got, err, tt.want, tt.wantErr)
		}
	}
}
