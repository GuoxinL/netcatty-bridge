package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	logPath     = `C:\sso_client\xftprun_probe.log`
	netcattyExe = `C:\Users\lgx31\AppData\Local\Programs\Netcatty\Netcatty.exe`
	origRunner  = `C:\WINDOWS\XFTP\xftp_run.exe`
	fallbackFlag = `C:\sso_client\forward_winscp.flag`
)

func logf(format string, args ...interface{}) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

// parse xftprun:p_ip=HOST:p_port=PORT:sid=SID:title=TITLE
func parseArgs(raw string) (host, port, sid, title string, ok bool) {
	raw = strings.TrimPrefix(raw, "xftprun:")
	raw = strings.TrimPrefix(raw, "//")
	for _, seg := range strings.Split(raw, ":") {
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "p_ip":
			host = kv[1]
		case "p_port":
			port = kv[1]
		case "sid":
			sid = kv[1]
		case "title":
			title = kv[1]
		}
	}
	ok = host != "" && sid != ""
	if port == "" {
		port = "22"
	}
	return
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isNetcattyRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq Netcatty.exe", "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "netcatty.exe")
}

func main() {
	logf("=== SFTP -> Netcatty bridge invoked ===")
	for i, arg := range os.Args[1:] {
		logf("argv[%d] = %q", i, arg)
	}
	if len(os.Args) < 2 {
		logf("ERROR: no args")
		return
	}
	raw := os.Args[1]
	host, port, sid, title, ok := parseArgs(raw)
	if !ok {
		logf("ERROR: parse failed")
		return
	}
	logf("parsed: host=%s port=%s sid=%s title=%s", host, port, sid, title)

	// SFTP 链路账号固定为 xftp；票据 sid 绑定目标节点与账号
	deepLink := fmt.Sprintf("ssh://xftp:%s@%s:%s", url.PathEscape(sid), host, port)
	logf("deepLink = %s", deepLink)

	// 可回退开关：放一个 forward_winscp.flag 文件即恢复原 WinSCP 链路
	if fileExists(fallbackFlag) {
		logf("FALLBACK flag present, forwarding to orig runner")
		cmd := exec.Command(origRunner, os.Args[1:]...)
		if err := cmd.Start(); err != nil {
			logf("ERROR forward failed: %v", err)
		} else {
			logf("forwarded to orig runner, pid=%d", cmd.Process.Pid)
		}
		return
	}

	if !fileExists(netcattyExe) {
		logf("ERROR: Netcatty.exe not found at %s", netcattyExe)
		return
	}
	if !isNetcattyRunning() {
		logf("WARN: Netcatty not running; skip to avoid cold-start white screen")
		return
	}

	cmd := exec.Command(netcattyExe, deepLink)
	cmd.Dir = filepath.Dir(netcattyExe)
	if err := cmd.Start(); err != nil {
		logf("ERROR send to Netcatty: %v", err)
		return
	}
	logf("sent deepLink to Netcatty, pid=%d", cmd.Process.Pid)
}
