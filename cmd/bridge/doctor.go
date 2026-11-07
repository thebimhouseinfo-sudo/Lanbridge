package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentlock"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentmcp"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

type doctorCheck struct {
	Name   string
	OK     bool
	Detail string
	Fix    string
}

// runDoctor checks each link in the chain (state dir, MCP lock, local client
// endpoint, host connection, project) and prints PASS/FAIL with a fix for each.
// It returns the process exit code: 0 when every check passes, 1 otherwise.
func runDoctor(out io.Writer) int {
	return printDoctorReport(out, collectDoctorChecks(agentmcp.ReadAuthFile, doctorHTTPClient()))
}

// printDoctorReport prints each check and returns the exit code.
func printDoctorReport(out io.Writer, checks []doctorCheck) int {
	failed := 0
	for _, check := range checks {
		mark := "PASS"
		if !check.OK {
			mark = "FAIL"
			failed++
		}
		fmt.Fprintf(out, "[%s] %s: %s\n", mark, check.Name, check.Detail)
		if !check.OK && check.Fix != "" {
			fmt.Fprintf(out, "       → %s\n", check.Fix)
		}
	}
	if failed > 0 {
		fmt.Fprintf(out, "\n%d kiểm tra thất bại.\n", failed)
		return 1
	}
	fmt.Fprintln(out, "\nTất cả kiểm tra đạt.")
	return 0
}

func doctorHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// collectDoctorChecks is separated from runDoctor so tests can inject the
// auth reader and HTTP client.
func collectDoctorChecks(readAuth func() (*agentmcp.AuthInfo, error), client *http.Client) []doctorCheck {
	var checks []doctorCheck

	dir, err := store.GetConfigDir()
	if err != nil {
		checks = append(checks, doctorCheck{"Thư mục trạng thái", false, err.Error(), "Kiểm tra biến môi trường HOME (macOS) hoặc APPDATA (Windows)."})
		return checks
	}
	checks = append(checks, doctorCheck{"Thư mục trạng thái", true, dir, ""})

	lockStatus, lockErr := agentlock.StatusDefault()
	switch {
	case lockErr != nil:
		checks = append(checks, doctorCheck{"Khóa MCP", false, lockErr.Error(), "Kiểm tra quyền ghi vào thư mục trạng thái."})
	case !lockStatus.Busy:
		checks = append(checks, doctorCheck{"Khóa MCP", true, "rảnh", ""})
	case lockStatus.Holder.PID == 0:
		checks = append(checks, doctorCheck{"Khóa MCP", false, "bị giữ nhưng không rõ pid", "Đóng các phiên Claude đang chạy bridge mcp, rồi thử lại."})
	case !agentlock.ProcessAlive(lockStatus.Holder.PID):
		checks = append(checks, doctorCheck{"Khóa MCP", false, fmt.Sprintf("pid %d đã chết nhưng khóa vẫn được giữ", lockStatus.Holder.PID), "Khóa do hệ điều hành quản lý; thử đóng hết phiên Claude rồi mở lại."})
	default:
		checks = append(checks, doctorCheck{"Khóa MCP", false, fmt.Sprintf("đang bị pid %d giữ", lockStatus.Holder.PID),
			fmt.Sprintf("Nếu đó là phiên Claude cũ: kill %d. Nếu đó là phiên đang dùng, không cần làm gì.", lockStatus.Holder.PID)})
	}

	auth, authErr := readAuth()
	if authErr != nil {
		checks = append(checks, doctorCheck{"LAN Bridge trên Mac", false, authErr.Error(), "Mở LAN Bridge trên Mac (ứng dụng hoặc `bridge client`)."})
		return checks
	}
	checks = append(checks, doctorCheck{"LAN Bridge trên Mac", true, fmt.Sprintf("đã mở, cổng %d", auth.Port), ""})

	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", auth.Port), 3*time.Second)
	if dialErr != nil {
		checks = append(checks, doctorCheck{"Kết nối nội bộ", false, dialErr.Error(), "LAN Bridge trên Mac đã thoát nhưng file còn sót. Mở lại LAN Bridge trên Mac."})
		return checks
	}
	_ = conn.Close()
	checks = append(checks, doctorCheck{"Kết nối nội bộ", true, "cổng nội bộ phản hồi", ""})

	status, statusErr := fetchBridgeStatus(client, auth)
	if statusErr != nil {
		checks = append(checks, doctorCheck{"Trạng thái bridge", false, statusErr.Error(), "Mở lại LAN Bridge trên Mac. Nếu vẫn lỗi, gửi thông báo này cho người phát triển."})
		return checks
	}
	checks = append(checks, doctorCheck{"Ghép nối", status.Paired, boolWord(status.Paired, "đã ghép nối", "chưa ghép nối"),
		"Ghép nối Mac với Windows trong LAN Bridge."})
	checks = append(checks, doctorCheck{"Kết nối tới Windows", status.Connected, orDash(status.Host),
		"Mở LAN Bridge trên Windows và kiểm tra cùng mạng LAN."})
	if status.Connected {
		checks = append(checks, doctorCheck{"Project", status.ProjectOpen, boolWord(status.ProjectOpen, "đang mở: "+status.ProjectID, "chưa mở project"),
			"Gọi windows_config action=open_project với project_id, hoặc mở project trong LAN Bridge."})
	}
	return checks
}

type bridgeStatusSummary struct {
	Paired      bool
	Connected   bool
	Host        string
	ProjectID   string
	ProjectOpen bool
}

func fetchBridgeStatus(client *http.Client, auth *agentmcp.AuthInfo) (bridgeStatusSummary, error) {
	body, _ := json.Marshal(map[string]any{"tool": "bridge_status", "args": map[string]any{}})
	url := fmt.Sprintf("http://127.0.0.1:%d/api/mcp/forward?t=%s", auth.Port, auth.Token)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return bridgeStatusSummary{}, fmt.Errorf("không gọi được bridge_status: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return bridgeStatusSummary{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return bridgeStatusSummary{}, fmt.Errorf("bridge_status trả HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result struct {
		StructuredContent struct {
			Paired      bool   `json:"paired"`
			Connected   bool   `json:"connected"`
			Host        string `json:"host"`
			ProjectID   string `json:"project_id"`
			ProjectOpen bool   `json:"project_open"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return bridgeStatusSummary{}, fmt.Errorf("bridge_status trả dữ liệu không hợp lệ: %w", err)
	}
	s := result.StructuredContent
	if s.Host == "" && !s.Connected && !s.Paired && s.ProjectID == "" && !s.ProjectOpen {
		// A reply with no status fields is the EMPTY_RESULT case; report it instead of "unpaired".
		return bridgeStatusSummary{}, fmt.Errorf("bridge_status trả kết quả rỗng")
	}
	return bridgeStatusSummary{Paired: s.Paired, Connected: s.Connected, Host: s.Host, ProjectID: s.ProjectID, ProjectOpen: s.ProjectOpen}, nil
}

func boolWord(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
