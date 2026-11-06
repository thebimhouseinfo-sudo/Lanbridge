package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentmcp"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/ui"
)

// openRunningUI opens the UI of an already running Mac client using the local
// endpoint file. It returns false when no live client is found.
func openRunningUI() (bool, error) {
	info, err := agentmcp.ReadAuthFile()
	if err != nil {
		return false, nil
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(info.Port))
	conn, err := net.DialTimeout("tcp", addr, 800*time.Millisecond)
	if err != nil {
		return false, nil
	}
	_ = conn.Close()
	return true, ui.OpenURL(fmt.Sprintf("http://%s/?t=%s", addr, info.Token))
}

// runUI implements `bridge ui`: open the control panel of the running client.
func runUI() {
	ok, err := openRunningUI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Không mở được giao diện: %v\n", err)
		os.Exit(1)
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "LAN Bridge chưa chạy. Mở app 'LAN Bridge' trước.")
		os.Exit(1)
	}
}
