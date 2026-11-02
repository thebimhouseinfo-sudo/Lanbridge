package ui

import (
	"log/slog"
	"runtime"
	"sync"

	"fyne.io/systray"
)

// TrayState represents the visual state of the tray icon
type TrayState int

const (
	TrayStateDisconnected TrayState = iota // Grey, unplug icon
	TrayStateReady                         // Green, cable icon
	TrayStateBusy                          // Blue, cable icon (running build/test)
	TrayStateWaiting                       // Orange, cable icon (waiting pair or pending permissions)
)

// TrayManager wraps systray lifecycle and menu actions
type TrayManager struct {
	mu           sync.Mutex
	currentState TrayState
	tooltip      string
	onOpenUI     func()
	onExit       func()
	ready        bool
}

// NewTrayManager initializes a tray manager
func NewTrayManager(onOpenUI func(), onExit func()) *TrayManager {
	return &TrayManager{
		currentState: TrayStateDisconnected,
		tooltip:      "LAN Test Bridge (Chưa kết nối)",
		onOpenUI:     onOpenUI,
		onExit:       onExit,
	}
}

// Run starts the systray main event loop (blocks the thread)
func (tm *TrayManager) Run() {
	systray.Run(tm.onReady, tm.onSystrayExit)
}

func (tm *TrayManager) onReady() {
	tm.mu.Lock()
	tm.ready = true
	tm.updateVisualStateLocked()
	tm.mu.Unlock()

	mOpen := systray.AddMenuItem("Mở giao diện", "Mở bảng điều khiển trên trình duyệt")
	mExit := systray.AddMenuItem("Thoát", "Dừng hẳn LAN Test Bridge")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				if tm.onOpenUI != nil {
					tm.onOpenUI()
				}
			case <-mExit.ClickedCh:
				systray.Quit()
			}
		}
	}()
}

func (tm *TrayManager) onSystrayExit() {
	if tm.onExit != nil {
		tm.onExit()
	}
}

// SetState updates tooltip and icon based on connection state
func (tm *TrayManager) SetState(state TrayState, tooltip string) {
	if state < TrayStateDisconnected || state > TrayStateWaiting {
		slog.Error("Invalid tray state", "state", state)
		return
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.currentState = state
	tm.tooltip = tooltip
	if tm.ready {
		tm.updateVisualStateLocked()
	}
	slog.Info("Tray status updated", "state", state, "tooltip", tooltip)
}

func (tm *TrayManager) updateVisualStateLocked() {
	icon, ok := iconForState(tm.currentState, runtime.GOOS)
	if !ok {
		slog.Error("Tray icon is unavailable", "state", tm.currentState, "platform", runtime.GOOS)
		return
	}
	systray.SetIcon(icon)
	systray.SetTooltip(tm.tooltip)
}

// Quit requests systray to exit
func (tm *TrayManager) Quit() {
	systray.Quit()
}
