# call-netcatty — 用深链唤起 Netcatty 连接堡垒机

## 核心结论

> curlrun 链不可控 → **绕过 curlrun** → 用 Netcatty 原生 `ssh://` 深链直连堡垒机 → SSH2 → 堡垒机自动跳内网节点

```
网页拿票据 → 取消 curlrun → ssh://user:票据@堡垒机:22 → Netcatty 接管 → 完事
```

## 成功的关键条件（缺一不可）

| # | 条件 | 说明 |
|---|------|------|
| ① | 堡垒机票据有效 | 每次从网页点登录，拿最新的 sid / 临时账号 |
| ② | 目标已绑定票据 | 票据背后绑定了内网节点（如 < internal-node >），堡垒机自动跳转，不需要额外传 target |
| ③ | Netcatty 已注册 `ssh://` 协议 | Mac 上 `open "ssh://..."` / 浏览器地址栏 / `<a href>` 都能唤起 |
| ④ | 深链格式正确 | `ssh://user:票据@堡垒机IP:端口` |
| ⑤ | 特殊字符 URL encode | 票据里的 `@` `#` `%` 等必须编码 |

## 一次完整连接的操作流

1. 浏览器打开堡垒机页面
2. 选好目标节点（< internal-node >）+ 协议（SSH2）+ 工具（X-Shell 随便选）
3. 点"登录" → 浏览器弹出 curlrun 确认框
4. 从 Network 面板 / 弹窗 URL 里抓取：
   - `fort_ip   = < fortress-host >`
   - `fort_port = 22`
   - `uid       = user`（或真实临时账号）
   - `sid       = < one-time-ticket >`（本次票据）
5. 取消 curlrun 弹窗
6. 浏览器地址栏输入：
   ```
   ssh://user:< one-time-ticket >@< fortress-host >:22
   ```
   （票据含特殊字符需 encode）
7. 回车 → Netcatty 唤起 → SSH2 握手 → 堡垒机鉴权 → 自动跳到 < internal-node > → 拿到 shell

## 为什么第一次成功、第二次失败

| 场景 | 原因 |
|------|------|
| 第一次成功 | 新票据 + 目标已绑定 + 参数完整 → 堡垒机代理正常加载策略 → 跳转成功 |
| 第二次失败 | 票据过期/复用 → 堡垒机解绑目标 → `target_protocol=null` → 代理空指针崩溃（code 139） |

**教训：每次连接必须用当次新生成的票据，不能复用上一次的 `ssh://` 链接。**

## 稳定性建议（排雷）

| 问题 | 对策 |
|------|------|
| 票据过期快 | 每次连接前重新点网页登录按钮拿新票据，不要复制旧深链 |
| 段错误 139 | 不是 Netcatty 的锅，是堡垒机 SecCmdProxy 3.1.0.2 的 bug；确保参数完整就不会触发 |
| 多标签干扰 | 每次只留一个连接流程，避免会话抢占 |
| 想更自动化 | 写浏览器插件，拦截 `curlrun://` → 自动解析 → 自动生成 `ssh://` → 自动拉 Netcatty，全程零手动 |

## 能否永久保存到本地 Netcatty 主机？（源码确认）

> 结论：**主机条目能永久保存；但票据/凭据不能、也不该永久保存。** "保存一次以后一键直连、不用再拿票据"做不到，这是堡垒机票据机制决定的，不是 Netcatty 的限制。

依据 Netcatty 源码 `application/app/AppSideEffects.tsx`（约 1610–1650 行）与 `domain/sshDeepLink.ts`：

### 深链分两条路径

| 深链形式 | 触发分支 | 行为 | 是否入库 |
|----------|----------|------|----------|
| `ssh://user:票据@堡垒机:22`（**带密码**，我们的场景） | `if (target.password)` | 一次性密码连接（`ephemeral`） | ❌ `savePassword: false`、`ephemeral: true`，**用完即弃** |
| `ssh://user@堡垒机:22`（不带密码） | `matchedEffectiveHost` 分支 | 用 Vault 里保存的凭据连接 | 复用已保存主机 |

### 带票据深链与 Vault 保存主机的交互（关键）

`buildSshDeepLinkEphemeralHostFromSaved` 的源码注释原文：

> keep the saved host's non-credential settings (proxy, jump chain, charset, ...) but authenticate with exactly the URL credentials, so vault identities and key references never override the one-time password.

即：
- ✅ 如果 Vault 里有**唯一匹配**的已保存主机，深链会复用它的**非凭据设置**（代理、跳板链、字符集等）
- ✅ 但**凭据一定用 URL 里的当次票据**——源码显式清空 `identityId` / `identityFileId` / `group`，防止 Vault 身份、密钥引用覆盖一次性密码
- 这是刻意设计：过期票据不会污染 Vault，避免"第二次失败"

### 匹配条件（`findSshDeepLinkHost`）

协议为 ssh + hostname 相同 + username 相同（若深链带）+ port 相同（默认 22），且**必须唯一命中**（`candidates.length === 1`）。

⚠️ **坑**：Vault 里如果有多条 `堡垒机IP:22 + 同用户名` 的记录，匹配会失败（返回 null），退化成纯临时连接，保存的设置全部不生效。**只保留一条堡垒机主机记录。**

### 推荐落地方式（两全其美）

1. 在 Vault 里**永久保存一条堡垒机主机**：`< fortress-host >:22` + 用户名，密码留空——用于固化跳链、字符集等非凭据配置
2. 每次连接仍用带当次新票据的深链：`ssh://user:新票据@< fortress-host >:22`
3. Netcatty 自动匹配这条保存记录，复用设置、用新票据认证——既免去重填 IP/端口，又不会踩过期票据

## 最稳方案

平台原生支持前，日常用 X-Shell + curlrun（不会踩空参），Netcatty 作备用/测试。
