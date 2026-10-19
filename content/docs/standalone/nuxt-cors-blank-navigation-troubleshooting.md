---
title: 刷新才出内容：一次「客户端跳转空白」的三层伪装排查实录
description: 本地开发时点击文章跳转为空、按 F5 才出内容，最后定位到后端 CORS 白名单把 localhost 写死、而浏览器用的是 127.0.0.1。复盘为什么刷新会好、为什么应用层一声不吭，以及 Gin 与 useAsyncData 两侧的预防写法。
date: 2026-10-19
series:
tags:
  - Nuxt
  - Go
  - Security
---

> 现象：客户端跳转到文章页，正文区一直停在骨架屏；按 F5 整页刷新，内容立刻出来。每次跳转都要刷一遍。
> 根因：后端 CORS 白名单里写的是 `http://localhost:3000`，而浏览器地址栏是 `http://127.0.0.1:3000` —— 在浏览器眼里这是两个不同的源，客户端请求全被 403 拒掉；刷新走的是 SSR 服务端取数，不经过 CORS，所以一切正常。

## 一、现象：只有跳转坏，刷新永远好

症状非常稳定，而且是**可复现的双态**：

| 操作                         | 结果                                                    |
| ---------------------------- | ------------------------------------------------------- |
| 首页点击"文档"→ 点击任意文章 | URL 变成 `/docs/<slug>`，页面框架正常，正文区骨架屏不动 |
| 在文章页按 F5                | 正文、标题、代码高亮全部正常                            |
| 再点浏览器后退 → 再点另一篇  | 又回到骨架屏不动                                        |

只要"刷新就好"，通常就说明**同一份数据、同一个接口，服务端拿得到、浏览器拿不到**。这条判断把排查范围砍掉了一半以上。（说"通常"是因为 SSR 也可能骗人：服务端有自己的缓存、自己的 baseURL、自己的内网出口 —— 那类问题的症状同样是"刷新就好"，但根因在服务端。）

> 复现环境：Nuxt 4.5.2 / Vue 3.5.41 / Go 1.26.4 / `gin-contrib/cors` v1.7.7 / Chrome 155。

## 二、先把前端的嫌疑逐个排掉

第一反应是 Nuxt 侧的经典坑，但每一条都解释不了"刷新就好"：

- **hydration mismatch**：没有任何 hydration 警告，且刷新后正常。
- **`__NUXT__` payload / 缓存复用**：详情页的 key 是 `doc:<url>`（含 slug），列表页是 `docs-list:<url>`（含分页与筛选参数），URL 一变必然重新请求 —— 可以排除 key 写死、数据被复用这类问题。
- **路由**：URL 变了、组件也换了、`Skeleton` 也渲染出来了 —— 路由没问题，是取数没结果。
- **Shiki 高亮**：高亮发生在 `useAsyncData` 的 `transform` 里，属于拿到数据之后的事。
- **浏览器缓存**：我一度怀疑是缓存，但在 Network 里只翻了请求列表、没看状态码那一列 —— 后来证明这一列才是答案。

有一点当时就该想清楚：**失效面到底是"某一条路由"还是"所有客户端取数"**。事后实测证明是后者 —— 在列表页点标签筛选同样拿不到数据（第九节：点 `#ORM` 是 0 张卡片，刷新同一 URL 却是 8 篇）。把失效面判断成"只有详情页坏"，本身就说明还没摸到根因。

## 三、三条手段，二十分钟定位

真正把方向掰回来的是三条很便宜的动作，建议按顺序做：

**① Network 面板看那条数据请求的状态码。** 我这里是 `403`。看到 403 应该把怀疑从"前端渲染 / hydration"转到"请求链路与后端中间件"—— 403 也可能是鉴权、限流、WAF，别急着下结论；用下一条带/不带 `Origin` 的对照来确认是不是 CORS 中间件主动拒绝的。

**② `curl` 对照：同一个 URL，带 `Origin` 头和不带。加 `-i` 把响应头一起打出来。**

```bash
# 服务端取数（SSR）的等价请求：不带 Origin
curl -i -s \
  "http://localhost:8080/api/docs?page=1&limit=1" | head -5
# → HTTP/1.1 200 OK

# 浏览器取数（CSR）的等价请求：带 Origin
curl -i -s \
  -H "Origin: http://127.0.0.1:3000" \
  "http://localhost:8080/api/docs?page=1&limit=1" | head -5
# → HTTP/1.1 403 Forbidden
```

只看状态码能区分 403 / 200；带上 `-i` 还能同时看到 `Access-Control-Allow-Origin` 有没有回 —— 有些框架不拒绝请求、只是不回这个头，那种情况状态码仍是 200，只有看响应头才能发现。

> 注意这两个东西不是一回事：URL 里的 `localhost:8080` 是**请求目标**（API 服务），
> `Origin` 头里的 `127.0.0.1:3000` 是**模拟浏览器的页面源**（前端页面所在的那个源）。
> `Origin` 永远描述"请求从哪个页面发出"，而不是"请求发往哪里" —— 这也是最容易看错的地方。

同一个接口，**加一个请求头就从 200 变成 403**，答案基本就到手了。

**③ 把"刷新就好"当线索，而不是当噪音。** 整页刷新和客户端跳转是两条完全不同的取数路径，两者唯一的共性是"数据来自同一个接口"。任何"刷新就好"的 bug，都先去比这两条路径的差异：

| 差异点             | 刷新（SSR）          | 客户端跳转（CSR） |
| ------------------ | -------------------- | ----------------- |
| 取数执行者         | Nitro 服务端（Node） | 浏览器            |
| 是否带 `Origin` 头 | 不带                 | 带                |
| 是否受 CORS 限制   | 不受                 | **受**            |
| Cookie / UA / IP   | 服务端视角           | 浏览器视角        |

不只是 CORS：`*.localhost` 与生产域名的 Cookie 作用域、服务端与浏览器不同的 User-Agent 触发的反爬、服务端能连内网而浏览器不能、只在构建期存在的环境变量 —— 这一整类问题的症状都是"刷新就好"。

## 四、根因：`localhost` 和 `127.0.0.1` 是两个源

`Origin` 的定义是 **scheme + host + port**（[MDN: Origin](https://developer.mozilla.org/en-US/docs/Glossary/Origin)），三者任一不同即为不同源。所以：

```text
http://localhost:3000      ← 白名单里写的
http://127.0.0.1:3000      ← 浏览器地址栏实际的
```

这两行在字符串上只差一点点，在浏览器眼里是**跨源**。

后端当时是这么写的：

```go
// 旧实现：默认放行来源写死为单个字面量
func corsOrigins() []string {
	const devDefaults = "http://localhost:3000"

	raw := os.Getenv("CORS_ORIGINS")
	if raw == "" {
		raw = devDefaults
	}
	// ...
}
```

而 `gin-contrib/cors` 对不在白名单里的 Origin 不是"少回一个响应头"，而是**直接中断请求**（`config.go`，v1.7.7）：

```go
if !cors.isOriginValid(c, origin) {
	c.AbortWithStatus(http.StatusForbidden)
	return
}
```

这就是为什么状态码是 403 —— 一个看起来像"鉴权失败"的状态码，实际上跟鉴权毫无关系。

## 五、为什么刷新就好：服务端取数没有"源"

CORS 是**浏览器**施加的策略，不是服务端鉴权。整页刷新时：

1. 浏览器请求文档 → Nitro 渲染页面；
2. Nitro 在服务端用 `$fetch('http://localhost:8080/api/docs')` 取数；
3. Node 的 `fetch` **不会**带 `Origin` 头（它不是浏览器，没有"当前页面源"的概念）；
4. CORS 中间件看到没有 `Origin`，按普通请求放行 → 200。

而客户端跳转时，第 2 步由浏览器执行，自动带上 `Origin: http://127.0.0.1:3000`，中间件判定不在白名单 → 403。

一句话：**刷新绕过了 CORS，而不是"刷新修好了什么"。**

## 六、第二层误导：应用层把失败伪装成"加载中"

如果页面在 403 时弹一个错误，我大概几分钟就找到原因了。但它没有。原因在前端：

```ts
// app/composables/useDocDetail.ts：只解构了 data，error 被丢掉
const { data: page } = useAsyncData<DocDetailBase>(/* ... */)
```

```vue
<!-- app/pages/docs/[slug].vue：page 为空时渲染骨架屏 -->
<Skeleton v-if="!page" />
```

列表页是另一种漏法：`useDocs()` 其实**返回了** `error`，但页面从来没解构它，于是同样停在 `<Skeleton v-if="!docs" />`。两条路径殊途同归 —— 详情页没接住错误，列表页接住了却没用。

结果就是：请求失败 → `page` 永远是 `undefined` → 骨架屏永远不动。用户（包括写代码的我）看到的不是"错误"，而是"加载中"，于是一路去查 hydration、查缓存、查高亮 —— 全在错误的方向上。

**但要说清"静默"的是哪一层**：浏览器控制台其实**有**报错，我实测时是 4 条：

```text
Access to fetch at 'http://localhost:8080/api/docs?...' from origin 'http://127.0.0.1:3000'
has been blocked by CORS policy: No 'Access-Control-Allow-Origin' header is present
on the requested resource.
```

它只说"缺了个响应头"，既没说"服务端其实回了 403"，也不指向页面的任何可见状态 —— 很容易被当成常规噪音划过去。真正一声不吭的是**应用层**：没有 toast、没有错误文案、没有降级，页面表现就是骨架屏不动。所以准确的说法是「**浏览器层喊了一声、应用层装作没听见**」，而不是"完全没有报错"。

修法很便宜，把失败态显式渲染出来：

```vue
<Skeleton v-if="!docs && !error" />
<div v-else-if="error" class="text-sm" style="color: var(--ui-danger)">
  {{ t("docs.loadFailed") }}
  <button type="button" @click="refresh()">{{ t("common.retry") }}</button>
</div>
```

> 这条教训比 CORS 本身更值得记：**"永远加载中"是比"报错"更贵的状态**。凡是 `useAsyncData` / `useFetch`，都把 `error` 一起解构掉。

## 七、隐藏触发点：端口顺延

还有一个同样安静的分支。Nuxt dev 发现 3000 被占用时会自动换端口：

```text
[get-port] Unable to find an available port (tried 3000 on host "localhost").
           Using alternative port 3001.
```

于是 `http://localhost:3001` 又是一个新 Origin，白名单再次失效。**只要白名单里写的是字面量 origin，任何一个"我以为一定是 3000"的假设都能让它静默失效**：

- 换个 host 打开（`localhost` ↔ `127.0.0.1` ↔ `[::1]`）
- 端口被占、顺延到 3001
- 同事本地起在 5173
- 手机真机调试走局域网 IP

## 八、修复：按规则放行本地开发，而不是列字面量

目标是"本地开发怎么打开都能用，生产一点不放宽"。生产是同源部署（Caddy 把 `/api` 反代到本服务），浏览器请求根本不跨源，所以 CORS 只需要为本地开发服务。

```go
// devOriginPattern 匹配本地开发前端的全部常见打开方式：
// localhost / 127.0.0.1 / [::1] 的任意端口。
var devOriginPattern = regexp.MustCompile(`^http://(localhost|127\.0\.0\.1|\[::1\]):[0-9]{1,5}$`)

// isDevEnv：只有显式声明 ENV=development 才算开发环境（为什么不能用 gin.Mode()，见下文）。
func isDevEnv() bool { return os.Getenv("ENV") == "development" }

func corsConfig() cors.Config {
	cfg := cors.Config{
		AllowMethods: []string{"GET", "OPTIONS"}, // 内容 API 只读
		AllowHeaders: []string{"Content-Type"},
	}

	// 显式白名单优先（与 AllowOriginFunc 互斥，见 cors.Config.Validate）
	if origins := splitList(os.Getenv("CORS_ORIGINS")); len(origins) > 0 {
		cfg.AllowOrigins = origins
		return cfg
	}

	// 不能留 nil：三个 origin 配置全空时 newCors 会直接 panic（见下文）
	cfg.AllowOriginFunc = func(origin string) bool {
		if !isDevEnv() {
			return false
		}
		return devOriginPattern.MatchString(origin)
	}
	return cfg
}
```

`splitList` 就是"逗号分割 + 去空白 + 丢掉空项"，与主线无关，实现从略。

几个要点：

- 用 `AllowOriginFunc` 而不是把 `AllowOrigins` 堆成 8 行字面量 —— 端口和主机名的组合是乘积关系，列不完。
- 保留 `CORS_ORIGINS` 显式覆盖，处理"真机调试走 `192.168.x.x`"这类确实列不进规则的情况。
- `[0-9]{1,5}` 只是**形态匹配**，不校验端口合法性（也能匹配 `localhost:99999`）—— 开发期白名单可以接受，浏览器根本产生不出这种 origin；`http://` 写死是因为本地走 http，用 https 调试就改成 `https?://`。

### 为什么"只按 CORS_ORIGINS 是否为空"来决定是不安全的

第一版修复漏了一步：它只看 `CORS_ORIGINS` 有没有配，没看当前是什么环境。后果是 —— **只要生产没配 `CORS_ORIGINS`，它就会放行任意 `http://localhost:<端口>`、`127.0.0.1`、`[::1]`**。这跟"生产一点不放宽"的说法直接矛盾，也意味着将来一旦给接口加上鉴权或 Cookie，这条规则会变成一扇朝本机页面敞开的后门。

所以把判断改成"**只有显式声明是开发环境才放行**"：`ENV=development` 之外（包括根本没设 `ENV`）一律拒绝。这样默认状态是关闭的，而不是打开。

顺带还有两个坑，都是写这段代码时才踩到的：

1. **不能靠 `gin.Mode()` 判断环境**。`gin.Default()` 默认就是 debug 模式，生产除非显式设置 `GIN_MODE=release`，否则会被判成开发环境 —— 等于把开发规则原样带到线上。
2. **不能用"不设置 origin 配置"来表达"关闭"**。`gin-contrib/cors` 在三个 origin 字段全空时会让 `Config.Validate()` 报错，`newCors` 随即 `panic`，服务直接起不来。这是"修安全性修到服务启动不了"的典型；正确写法是给一个恒返回 `false` 的函数。

配套还有两件小事：`.env.example` 必须带上 `ENV=development`（否则新同学复制出来的 `.env` 会命中断言、本地请求全 403，等于把这篇讲的 bug 原样还给他）；容器里显式写 `ENV=production`，别依赖"忘了设就是生产"这种隐含约定。启动时再打一行告警，把"跨源全拒"这种配置摊到日志上 —— `⚠️ CORS: 未配置 CORS_ORIGINS 且 ENV≠development —— 所有跨源请求都会被拒绝（同源部署无影响）`。

## 九、验证：同一份前端，只切后端配置

修复前后各跑了一遍浏览器实测（同一套前端、同一份内容，只改后端 CORS 配置）：

| 场景                                      | 首屏 `/docs` | 点卡片进详情（客户端导航）       | 按 F5 刷新            |
| ----------------------------------------- | ------------ | -------------------------------- | --------------------- |
| 修复前（只放行 `localhost:3000`）         | 1700 字符    | **36 字符，无标题、无代码块**    | 5328 字符，8 个代码块 |
| 修复后（`ENV=development`，放行本地端口） | 1700 字符    | **5328 字符，标题 + 8 个代码块** | 5328 字符，8 个代码块 |

列表页这一组更能说明问题 —— 同一个筛选条件，点出来是空，刷新出来是满：

| 场景                                | 修复前                     | 修复后   |
| ----------------------------------- | -------------------------- | -------- |
| `/docs` 点面板 `#ORM`（客户端筛选） | **0 张卡片、5 个骨架占位** | 8 张卡片 |
| 刷新同一 URL（`?tag=ORM`）          | 8 张卡片                   | 8 张卡片 |

「36 字符」就是骨架屏加导航栏的文本量 —— 内容一个字都没进来。修复后客户端跳转与刷新的正文长度**完全一致**。

> 测量口径：两处「字符」都取自 `document.body.innerText.length`，是 UTF-16 code unit 计数，**不是字节数**（中文按 UTF-8 字节算约为 3 倍）。要比对内容量，务必用同一口径。

CORS 行为矩阵（**本地开发配置**：`ENV=development` 且未设 `CORS_ORIGINS`）：

```text
http://localhost:3000     200  Access-Control-Allow-Origin: http://localhost:3000
http://127.0.0.1:3000     200  Access-Control-Allow-Origin: http://127.0.0.1:3000   ← 修复前 403
http://localhost:3001     200  Access-Control-Allow-Origin: http://localhost:3001   ← 修复前 403
http://127.0.0.1:3001     200  Access-Control-Allow-Origin: http://127.0.0.1:3001
http://[::1]:3000         200  Access-Control-Allow-Origin: http://[::1]:3000
http://evil.example.com   403  （仍然拦）
null                      403  （仍然拦）
无 Origin（SSR / curl）    200
OPTIONS 预检               204  + 正确头部
```

同一份代码换成**生产配置**（`ENV=production`、同样不设 `CORS_ORIGINS`）再跑一遍：上面前五行**全部变成 403**，"无 Origin" 仍是 200，服务正常启动并打出那行告警 —— 这就是环境门要保证的事。生产若真有跨源调用方（移动端、第三方、独立域名），显式配 `CORS_ORIGINS` 即可，此时它优先于环境门。

> 矩阵里的 `null` 是"浏览器发不出正常 Origin"的情形：沙箱化 iframe、`file://` 页面等，浏览器会把 `Origin` 头直接发成字面量字符串 `null`。它同样应该被拒 —— 否则任何本地 HTML 文件都能读到你的接口。

## 十、可迁移的清单

遇到"客户端跳转空白 / 刷新就好"，按这个顺序过一遍，比翻代码快得多：

1. **先看 Network 的 Status 和 Response Headers**：403、CORS、`net::ERR_FAILED` 的处置完全不同。控制台红字只说明"浏览器侧被 CORS 拦了"，它分不清服务端是 403 还是 200 但缺 `Access-Control-Allow-Origin` —— 这两种情况的下一步动作完全不一样，必须回到 Network 面板看。
2. **`curl` 加/不加 `Origin` 对照**。这是判断"CORS 还是别的"的唯一低成本手段。
3. **核对地址栏与白名单的字面量**：`localhost` / `127.0.0.1` / `[::1]`、`www` 与非 `www`、`http` 与 `https`、端口。这些在字符串上都很像，在浏览器眼里全都是不同的源。
4. **端口不要写死**。dev server 会顺延，代理会改端口，容器会映射端口。
5. **Vite `server.proxy` 是个"消音器"**：走代理时请求同源，CORS 不参与；一旦为了排查删掉代理直连后端，问题才暴露出来 —— 所以"本地正常、联调就炸"多半也是这个。
6. **把 `useAsyncData` 的 `error` 渲染出来**，别让失败态伪装成"加载中"。
7. **把这张 Origin 矩阵写进测试**。`corsConfig()` 是纯函数，用 `httptest` 表驱动断言 `localhost` / `127.0.0.1` / `[::1]` / `evil.example.com` / `null` / 无 Origin 的结果，再加一条"生产配置下 `localhost` 必须被拒"和"`cors.New` 不能 panic"。这类白名单是最容易被后人"顺手放宽"的地方，手动验证挡不住回归。

## 十一、参考

- [MDN: Origin](https://developer.mozilla.org/en-US/docs/Glossary/Origin) —— `scheme + host + port` 的权威定义
- [MDN: 同源策略](https://developer.mozilla.org/en-US/docs/Web/Security/Same-origin_policy)
- [gin-contrib/cors](https://github.com/gin-contrib/cors) —— `AllowOrigins` 与 `AllowOriginFunc` 的互斥约束
- 同类现象的社区讨论（可作为"这个问题有多常见"的佐证，但大多停在"配上 CORS"这一步）：
  - [Nuxt: data loads on refresh but not on navigation](https://stackoverflow.com/questions/79059932)
  - [Nuxt 3 + Directus: CORS error when navigating](https://stackoverflow.com/questions/79079249/nuxt-3-directus-returns-cors-error-when-navigating-the-site)
  - [每次路由切换后 asyncData 跨域报错（刷新后正常）](https://blog.csdn.net/xiaoqihshw/article/details/144127933)

> 附记：这个问题之所以值得写下来，不是因为 CORS 难，而是因为它的三层伪装——后端用 403 把自己伪装成鉴权问题、前端用骨架屏把失败伪装成加载中、而"刷新就好"把人往缓存和 hydration 上引。三层里任何一层把话说明白，这段排查就能压到二十分钟以内。
