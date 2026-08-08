Miniflux 2
==========

Miniflux is a minimalist and opinionated feed reader.
It's simple, fast, lightweight and super easy to install.

Official website: <https://miniflux.app>

Features
--------

### Feed Reader

- Supported feed formats: Atom 0.3/1.0, RSS 1.0/2.0, and JSON Feed 1.0/1.1.
- [OPML](https://en.wikipedia.org/wiki/OPML) file import/export and URL import.
- Supports multiple attachments (podcasts, videos, music, and images enclosures).
- Plays videos from YouTube directly inside Miniflux.
- Organizes articles using categories and bookmarks.
- Share individual articles publicly.
- Fetches website icons (favicons).
- Saves articles to third-party services.
- Provides full-text search (powered by Postgres).
- Available in 20 languages: Portuguese (Brazilian), Chinese (Simplified and Traditional), Dutch, English (US), Finnish, French, German, Greek, Hindi, Indonesian, Italian, Japanese, Polish, Romanian, Russian, Taiwanese POJ, Ukrainian, Spanish, and Turkish.

### Privacy and Security

- Removes pixel trackers.
- Strips tracking parameters from URLs (e.g., `utm_source`, `utm_medium`, `utm_campaign`, `fbclid`, etc.).
- Retrieves original links when feeds are sourced from FeedBurner.
- Opens external links with attributes `rel="noopener noreferrer" referrerpolicy="no-referrer"` for improved security.
- Implements the HTTP header `Referrer-Policy: no-referrer` to prevent referrer leakage.
- Provides a media proxy to avoid tracking and resolve mixed content warnings when using HTTPS.
- Plays YouTube videos via the privacy-focused domain `youtube-nocookie.com`.
- Supports alternative YouTube video players such as [Invidious](https://invidio.us).
- Blocks external JavaScript to prevent tracking and enhance security.
- Sanitizes external content before rendering it.
- Enforces a [Content Security](https://developer.mozilla.org/en-US/docs/Web/HTTP/CSP) and a [Trusted Types Policy](https://developer.mozilla.org/en-US/docs/Web/API/Trusted_Types_API) to only application JavaScript and blocks inline scripts and styles. 

### Bot Protection Bypass Mechanisms

- Optionally disable HTTP/2 to mitigate fingerprinting.
- Allows configuration of a custom user agent.
- Supports adding custom cookies for specific use cases.
- Enables the use of proxies for enhanced privacy or bypassing restrictions.

### Content Manipulation

- Fetches the original article and extracts only the relevant content using a local Readability parser.
- Allows custom scraper rules based on <abbr title="Cascading Style Sheets">CSS</abbr> selectors.
- Supports custom rewriting rules for content manipulation.
- Provides a regex filter to include or exclude articles based on specific patterns.
- Optionally permits self-signed or invalid certificates (disabled by default).
- Scrapes YouTube's website to retrieve video duration as read time or uses the YouTube API (disabled by default).

### User Interface

- Optimized stylesheet for readability.
- Responsive design that adapts seamlessly to desktop, tablet, and mobile devices.
- Minimalistic and distraction-free user interface.
- No requirement to download an app from Apple App Store or Google Play Store.
- Can be added directly to the home screen for quick access.
- Supports a wide range of keyboard shortcuts for efficient navigation.
- Optional touch gesture support for navigation on mobile devices.
- Custom stylesheets and JavaScript to personalize the user interface to your preferences.
- Themes:
    - Light (Sans-Serif)
    - Light (Serif)
    - Dark (Sans-Serif)
    - Dark (Serif)
    - System (Sans-Serif) – Automatically switches between Dark and Light themes based on system preferences.
    - System (Serif)

### Integrations

- 25+ integrations with third-party services: [Apprise](https://github.com/caronc/apprise), [Betula](https://sr.ht/~bouncepaw/betula/), [Cubox](https://cubox.cc/), [Discord](https://discord.com/), [Espial](https://github.com/jonschoning/espial), [Instapaper](https://www.instapaper.com/), [LinkAce](https://www.linkace.org/), [Linkding](https://github.com/sissbruecker/linkding), [LinkTaco](https://linktaco.com), [LinkWarden](https://linkwarden.app/), [Matrix](https://matrix.org), [Notion](https://www.notion.com/), [Ntfy](https://ntfy.sh/), [Nunux Keeper](https://keeper.nunux.org/), [Pinboard](https://pinboard.in/), [Pushover](https://pushover.net), [RainDrop](https://raindrop.io/), [Readeck](https://readeck.org/en/), [Readwise Reader](https://readwise.io/read), [RssBridge](https://rss-bridge.org/), [Shaarli](https://github.com/shaarli/Shaarli), [Shiori](https://github.com/go-shiori/shiori), [Slack](https://slack.com/), [Telegram](https://telegram.org), [Wallabag](https://www.wallabag.org/), etc.
- Bookmarklet for subscribing to websites directly from any web browser.
- Webhooks for real-time notifications or custom integrations.
- Compatibility with existing mobile applications using the Fever or Google Reader API.
- REST API with client libraries available in [Go](https://github.com/miniflux/v2/tree/main/client) and [Python](https://github.com/miniflux/python-client).

### Authentication

- Local username and password.
- Passkeys ([WebAuthn](https://en.wikipedia.org/wiki/WebAuthn)).
- Google (OAuth2).
- Generic OpenID Connect.
- Reverse-Proxy authentication.

### Technical Stuff

- Written in [Go (Golang)](https://golang.org/).
- Single binary compiled statically without dependency.
- Works only with [PostgreSQL](https://www.postgresql.org/).
- Does not use any ORM or any complicated frameworks.
- Uses modern vanilla JavaScript only when necessary.
- All static files are bundled into the application binary using the Go `embed` package.
- Supports the Systemd `sd_notify` protocol for process monitoring.
- Configures HTTPS automatically with Let's Encrypt.
- Allows the use of custom <abbr title="Secure Sockets Layer">SSL</abbr> certificates.
- Supports [HTTP/2](https://en.wikipedia.org/wiki/HTTP/2) when TLS is enabled.
- Updates feeds in the background using an internal scheduler or a traditional cron job.
- Uses native lazy loading for images and iframes.
- Compatible only with modern browsers.
- Adheres to the [Twelve-Factor App](https://12factor.net/) methodology.
- Provides official Debian/RPM packages and pre-built binaries.
- Publishes a Docker image to Docker Hub, GitHub Registry, and Quay.io Registry, with ARM and RISC-V architecture support.
- Uses a limited amount of third-party go dependencies
- Has a comprehensive testsuite, with both unit tests and integration tests.
- Only uses a couple of MB of memory and a negligible amount of CPU, even with several hundreds of feeds.
- Respects/sends Last-Modified, If-Modified-Since, If-None-Match, Cache-Control, Expires and ETags headers, and has a default polling interval of 1h.

Documentation
-------------

The Miniflux documentation is available here: <https://miniflux.app/docs/> ([Man page](https://miniflux.app/miniflux.1.html))

- [Opinionated?](https://miniflux.app/opinionated.html)
- [Features](https://miniflux.app/features.html)
- [Requirements](https://miniflux.app/docs/requirements.html)
- [Installation Instructions](https://miniflux.app/docs/installation.html)
- [Upgrading to a New Version](https://miniflux.app/docs/upgrade.html)
- [Configuration](https://miniflux.app/docs/configuration.html)
- [Command Line Usage](https://miniflux.app/docs/cli.html)
- [User Interface Usage](https://miniflux.app/docs/ui.html)
- [Keyboard Shortcuts](https://miniflux.app/docs/keyboard_shortcuts.html)
- [Integration with External Services](https://miniflux.app/docs/#integrations)
- [Rewrite and Scraper Rules](https://miniflux.app/docs/rules.html)
- [API Reference](https://miniflux.app/docs/api.html)
- [Development](https://miniflux.app/docs/development.html)
- [Internationalization](https://miniflux.app/docs/i18n.html)
- [Frequently Asked Questions](https://miniflux.app/faq.html)

Screenshots
-----------

Default theme:

![Default theme](https://miniflux.app/images/overview.png)

Dark theme when using keyboard navigation:

![Dark theme](https://miniflux.app/images/item-selection-black-theme.png)

Credits
-------

- Authors: Frédéric Guillot - [List of contributors](https://github.com/miniflux/v2/graphs/contributors)
- Distributed under Apache 2.0 License


# 本地启动

## go 相关命令

```

go build -o miniflux.exe .

# 注入版本号的 ldflags
go build -ldflags="-X 'miniflux.app/v2/internal/version.Version=dev'" -o miniflux.exe .

```


go 相关的其它命令：
```
go env GOPROXY

# 测试下载模块
go mod download
go mod tidy

# 清缓存
go clean -modcache
go clean -cache

# 确认 gopls
gopls version

# 如没有 gopls 执行安装命令：
go install golang.org/x/tools/gopls@latest
```

PG 建库：

```
> cd C:\Program Files\PostgreSQL\16\bin
> .\psql.exe -U postgres -c "CREATE DATABASE miniflux;"
> .\psql.exe -U postgres -d miniflux -c "CREATE EXTENSION IF NOT EXISTS hstore;"


```

【tasks.json 是跑命令，launch.json 是起调试器（底层是 dlv）】

对 .vscode 下 tasks.json 的使用：
```
1. Ctrl+Shift+P
2. 输入 Tasks: Run Task（中文界面是"任务: 运行任务"）
3. 回车后弹出任务列表，选你要的 label
4. 停止任务：Ctrl+Shift+P → Tasks: Terminate Task。或者直接在任务终端里 Ctrl+C。
5. 重跑上一个：Ctrl+Shift+P → Tasks: Rerun Last Task。命令面板里最近跑过的任务也会置顶显示。
```

单独执行命令：Set-ExecutionPolicy -Scope CurrentUser RemoteSigned，输 Y 回车即可。
这条命令写的是注册表：HKCU:\Software\Microsoft\PowerShell\1\ShellIds\Microsoft.PowerShell\ExecutionPolicy
永久生效，跟着你的 Windows 用户账户走。以后新开终端、重装 VS Code、重启电脑都不用再执行。所以确实是"一次性"的。

对 .vscode 下 launch.json 的使用：
```
调试使用
```

## 启动脚本

1. 第一个可用版本：dev++.ps1.v1

2. 升级后第二个版本：dev++.ps1，使用 Cloudflare Tunnel 代理本地服务，可用域名 https://miniflux.asoidhfoas.ltd 进行公网访问本地服务 

```
[dev] DB       = postgres://postgres:****@127.0.0.1:5432/miniflux?sslmode=disable
[dev] LISTEN   = 127.0.0.1:8080
[dev] BASE_URL = https://miniflux.asoidhfoas.ltd
[dev] TUNNEL   = https://miniflux.asoidhfoas.ltd -> http://127.0.0.1:8080
[dev] Go       = go version go1.26.2 windows/amd64

level=INFO msg="Running database migrations" current_version=133 latest_version=133
level=DEBUG msg="Starting daemon..."
level=DEBUG msg="Starting background scheduler..."
level=DEBUG msg="Worker started" worker_id=4
level=DEBUG msg="Worker started" worker_id=1
level=DEBUG msg="Worker started" worker_id=5
level=DEBUG msg="Worker started" worker_id=0
level=DEBUG msg="Worker started" worker_id=3
level=DEBUG msg="Worker started" worker_id=7
level=DEBUG msg="Worker started" worker_id=2
level=DEBUG msg="Worker started" worker_id=9
level=DEBUG msg="Worker started" worker_id=8
level=DEBUG msg="Worker started" worker_id=6
level=INFO msg="Starting HTTP server" listen_address=127.0.0.1:8080
```

注意：

如果执行 dev++.ps1 脚本报错如下：
```
> .\dev++.ps1
.\dev++.ps1 : File D:\WORKSPACE\all-monitor-space\miniflux-v2\dev++.ps1 cannot be loaded. The file D:\WORKSPACE\all-monitor-space\mi
niflux-v2\dev++.ps1 is not digitally signed. You cannot run this script on the current system. For more information about running sc
ripts and setting execution policy, see about_Execution_Policies at https:/go.microsoft.com/fwlink/?LinkID=135170.
At line:1 char:1
+ .\dev++.ps1
+ ~~~~~~~~~~~
+ CategoryInfo          : SecurityError: (:) [], PSSecurityException
+ FullyQualifiedErrorId : UnauthorizedAccess
```

执行如下命令解除这个文件的阻止：
```
Unblock-File -LiteralPath ".\dev++.ps1"

# 检查是否仍有互联网标记
Get-Item ".\dev++.ps1" -Stream *

```


# 订阅本地服务

对于如果要订阅本地启动的 rsshub 中的服务，需要开启如下配置(.env.dev + packaging/miniflux.conf)：
```
# 解除默认阻止 fetcher 访问私有网络和回环地址
FETCHER_ALLOW_PRIVATE_NETWORKS=1
```

## Error feed refresh（错误源自动刷新）

后台 worker 可定期串行刷新仍处于解析错误状态的 feed。通过环境变量控制（`dev++.ps1` 会从 `.env.dev` 加载）：

```
# 单位：分钟。0=禁用（默认）；例如 30 表示每 30 分钟扫描一次
ERROR_REFRESH_INTERVAL=30
```

行为说明：
- `ERROR_REFRESH_INTERVAL=0` 时关闭该循环（默认，便于测试）
- 开启后与主进程同启同停（worker pool shutdown 一并结束）
- 错误源串行刷新，相邻 feed 之间随机退避 3–5 秒，降低对目标站的压力


