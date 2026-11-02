//go:build !windows

package appcontrol

import (
	"errors"
)

type platformController struct{}

func newPlatformController() Controller {
	return platformController{}
}

func (platformController) Start(string) error {
	return errors.New("Start/Stop is supported only on Windows")
}

func (platformController) Stop(string) (StopResult, error) {
	return StopResult{}, errors.New("Start/Stop is supported only on Windows")
}
