# SFTP 登录自动接入 Netcatty（xftprun 桥接）

> 目标：从算力平台点 SFTP 登录，不再弹 WinSCP（省掉主机密钥确认框），直接让 Netcatty 建连并连到内网节点。
> 状态：已验证成功（2026-11-19）。

## 一、SFTP 链路与 SSH 链路的本质区别

| 项 | SSH 登录 | SFTP 登录 |
|---|---|---|
| 浏览器协议 | `curlrun://` | `xftprun://` |
| 第一环程序 | `C:\sso_client\curlrun.exe` | `C:\WINDOWS\XFTP\xftp_run.exe` |
| SSO 配置 | 填 IP/协议/口令 | 填 IP/口令/工具=XFTP |
| 原始终端 | Xshell | WinSCP |
| 票据位置 | `Xshell.exe -url ssh://user:<票据>@堡垒机:22` | `xftprun` URL 参数里的 `sid` |

## 二、抓到的真实调用格式（关键）

### 浏览器传给 xftp_run.exe 的参数

```
xftprun:p_ip=<堡垒机IP>:p_port=22:sid=<当次票据>:title=<内网节点>_xftp
```

实测样例（已脱敏）：

```
xftprun:p_ip=FORTRESS_HOST:p_port=22:sid=ONE_TIME_TICKET:title=INTERNAL_NODE_xftp
```

四个字段含义：

- `p_ip` — 堡垒机 IP
- `p_port` — 堡垒机 SSH 端口（22）
- `sid` — **当次动态票据**（每次点登录都重新生成，不能复用）
- `title` — 内网节点 + `_xftp` 后缀（就是 Netcatty 里显示的会话名）

### 原链路 WinSCP 命令行（对照）

```
WinSCP.exe sftp://xftp:<票据>@<堡垒机>:22 /sessionname="<内网节点>_xftp" /newinstance
```

## 三、为什么拦截点选 `xftprun://` 协议，而不是 WinSCP

| 拦截点 | 位置 | 可行性 |
|---|---|---|
| 改 `WinSCP.exe` | `C:\WINDOWS\XFTP\winscp\` | ❌ 系统目录，当前用户非管理员，无写权限 |
| 改 `xftp.exe` | `C:\sso_client\` | ⚠️ NSIS 自解压 Java 程序，参数格式未知 |
| **改 `xftprun` 协议注册** | HKCU | ✅ 用户级覆盖即可，票据已在 URL 里，一步到位 |

做法是：在 **HKCU**（用户级，不动系统级的 HKCR）覆盖 `xftprun` 协议 command，指向我们自己的桥接程序。

## 四、Netcatty 深链格式——一个反直觉的点

Netcatty 源码里深链 scheme 只声明了三种：`ssh://` / `telnet://` / `jms://`，**没有 `sftp://`**。

但它内部有一段关键逻辑：

```
protocol==="sftp" ? {autoOpenSftpPanel: true} : {}
```

也就是说：

- SFTP 面板是 SSH 会话的一个"自动打开"行为，不是独立协议入口
- `protocol:"sftp"` 由 **JMS 深链**的 JSON 字段触发，但 JMS 深链默认关闭（`defaultWhenMissing: false`），且与 JumpServer 客户端冲突

**最终选择**：不用 JMS，直接用 `ssh://` 深链（复用已跑通的 SSH 链路），配合一个全局设置自动弹面板。

生成的深链：

```
ssh://xftp:<sid>@<堡垒机IP>:<p_port>
```

> 用户名用 `xftp`（不是 SSH 链路的 `user`）——这是堡垒机给 SFTP 工具分配的账号，票据 `sid` 与它绑定。

## 五、部署清单

### 1. 桥接程序

- 位置：`C:\sso_client\xftprun_probe.exe`（Go 交叉编译，源码见 `bridge/main.go`）
- 逻辑：
  1. 记录收到的参数到 `C:\sso_client\xftprun_probe.log`
  2. 解析 `p_ip` / `p_port` / `sid` / `title`
  3. 生成 `ssh://xftp:<sid>@<host>:<port>`
  4. 检测 Netcatty 是否运行——**运行**：`Netcatty.exe <url>` 走 second-instance 交给它；**未运行**：跳过（避免冷启动白屏）
  5. **不再启动 WinSCP**

### 2. 协议注册（HKCU 覆盖）

```powershell
$key = 'HKCU:\Software\Classes\xftprun\shell\open\command'
New-Item -Path $key -Force | Out-Null
Set-ItemProperty -Path $key -Name '(default)' -Value '"C:\sso_client\xftprun_probe.exe" "%1"'
```

- 原 HKCR 系统级注册不动：`xftprun://` → `C:\WINDOWS\XFTP\xftp_run.exe "%1"`
- 用户级 HKCU 优先，浏览器走我们的桥接

### 3. Netcatty 全局设置（必做一次）

设置 → SFTP → 打开 **Auto-open sidebar on connect**（连接时自动打开 SFTP 侧边栏）。

不打开的话：`ssh://` 深链会连上，但不会自动弹文件面板，需要你手动点开 SFTP 面板。

### 4. 回退开关

如果 Netcatty 连不上、需要恢复原 WinSCP 链路：

```
新建空文件 C:\sso_client\forward_winscp.flag
```

桥接程序检测到该文件存在，就把参数原样转发给 `C:\WINDOWS\XFTP\xftp_run.exe`，恢复原行为。删掉该文件即重新走 Netcatty。

## 六、验证结果（2026-11-19）

```
[17:21:06] === SFTP -> Netcatty bridge invoked ===
[17:21:06] argv[0] = "xftprun:p_ip=<堡垒机>:p_port=22:sid=<票据>:title=<节点>_xftp"
[17:21:06] parsed: host=<堡垒机> port=22 sid=<票据> title=<节点>_xftp
[17:21:06] deepLink = ssh://xftp:<票据>@<堡垒机>:22
[17:21:07] sent deepLink to Netcatty, pid=78216
```

- ✅ 连上内网节点，WinSCP 无残留进程
- ⚠️ **第一次失败、重试成功**：失败在 **Netcatty 内部**，不在桥接。桥接日志显示 17:21:07 已成功把 URL 交给 Netcatty（SSH 连上了）；但 Netcatty 自动开 SFTP 面板时，堡垒机 SFTP 子系统报 `Received exit code 139 while establishing SFTP session` / `(SSH) Channel open failure: open failed`（Netcatty crash log `09:23:34Z` = 本地 17:23:34，`origin: sftpBridge.openSftpForSession`）。这是**堡垒机 SecCmdProxy 的偶发 bug**（exit 139 段错误，与 SSH 链路遇到的是同一个），Netcatty 会自动 `falling back to SCP mode (auto)`，点重试后堡垒机正常响应即成功。客户端无法根治，重试即可。

## 七、已知限制

- 票据 `sid` 是一次性的，每次必须重新从平台点登录拿新票据，不能保存
- Netcatty 必须处于运行状态（靠它自身内置自启常驻）；冷启动带 URL 会白屏，桥接程序因此跳过冷启动
- "自动弹 SFTP 面板"依赖全局设置，需在 UI 手动勾选一次
- 堡垒机 SFTP 子系统偶发 exit 139（channel open failure），首次连接可能失败，点重试即可；这是堡垒机侧 bug，不是桥接或 Netcatty 的问题
