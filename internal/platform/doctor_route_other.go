//go:build !linux && !darwin
// +build !linux,!darwin

package platform

import (
	"context"
	"errors"
)

func probeDefaultRoute(context.Context) (doctorRoute, error) {
	return doctorRoute{}, errors.New("当前平台不支持默认路由检测")
}
