package ui

import (
	_ "embed"
)

type trayIconAsset struct {
	png []byte
	ico []byte
}

//go:embed icons/cable-green.png
var cableGreenPNG []byte

//go:embed icons/cable-green.ico
var cableGreenICO []byte

//go:embed icons/cable-blue.png
var cableBluePNG []byte

//go:embed icons/cable-blue.ico
var cableBlueICO []byte

//go:embed icons/cable-orange.png
var cableOrangePNG []byte

//go:embed icons/cable-orange.ico
var cableOrangeICO []byte

//go:embed icons/unplug-gray.png
var unplugGrayPNG []byte

//go:embed icons/unplug-gray.ico
var unplugGrayICO []byte

var trayIconAssets = map[TrayState]trayIconAsset{
	TrayStateDisconnected: {png: unplugGrayPNG, ico: unplugGrayICO},
	TrayStateReady:        {png: cableGreenPNG, ico: cableGreenICO},
	TrayStateBusy:         {png: cableBluePNG, ico: cableBlueICO},
	TrayStateWaiting:      {png: cableOrangePNG, ico: cableOrangeICO},
}

func iconForState(state TrayState, platform string) ([]byte, bool) {
	assets, ok := trayIconAssets[state]
	if !ok {
		return nil, false
	}
	if platform == "windows" {
		return assets.ico, len(assets.ico) > 0
	}
	return assets.png, len(assets.png) > 0
}
