---
title: Nuxt 4 博客 Sitemap 配置完整指南
description: 介绍了 Nuxt 4 博客 Sitemap 配置的基础知识、静态 Sitemap 和动态 Sitemap 的实现方法，并提供了生产环境部署要点。
date: 2026-02-04
series: ecosystem
level: P1
tags:
  - Nuxt
  - SEO
  - Engineering
---

## 📚 系列导航

本系列共两篇：

1. [**Nuxt 集成 RSS 服务完全指南**](./nuxt-rss-guide) —— 从模块到手写，构建完全可控的 RSS/Atom/JSON Feed
2. [**Nuxt 4 博客 Sitemap 配置完整指南**](./nuxt-sitemap-guide) —— 静态与动态 Sitemap 的完整配置

---

## 为什么需要 Sitemap？

Sitemap（站点地图）是一个 XML 文件，它告诉搜索引擎你的网站上有哪些页面、这些页面的重要性以及更新频率。对于个人博客而言，Sitemap 能：

- **加速新内容收录**：新发布的文档可以在几小时到几天内被搜索引擎发现
- **提高内容覆盖率**：确保所有文档都被索引，避免“孤岛页面”
- **优化爬虫效率**：合理分配搜索引擎抓取资源

## 基础配置方案

### 方案一：静态 Sitemap（最简单）

适用于文档数量少、更新频率低的博客。

在 `public/` 目录创建 `sitemap.xml`：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://moongate.top/</loc>
    <priority>1.0</priority>
    <changefreq>daily</changefreq>
  </url>
  <url>
    <loc>https://moongate.top/about</loc>
    <priority>0.5</priority>
    <changefreq>monthly</changefreq>
  </url>
  <!-- 手动添加其他文档 -->
</urlset>
```

在 `robots.txt` 中声明：

```text
Sitemap: https://moongate.top/sitemap.xml
```

### 方案二：动态 Sitemap（推荐）

使用 Nuxt 3 服务器路由动态生成，适合经常更新的博客。

## 完整实现步骤

### 1. 创建动态 Sitemap 端点

```typescript
// server/routes/sitemap.xml.ts
export default defineEventHandler(async (event) => {
  const siteUrl = useRuntimeConfig().public.siteUrl

  try {
    // 1. 获取文档数据
    const docs = await queryCollection(event, "docs").select("path").all()
    const about = await queryCollection(event, "about").select("path").all()

    // 2. 构建URL数组
    const urls = [
      `${siteUrl}`,
      `${siteUrl}/docs`,
      `${siteUrl}/about`,
      `${siteUrl}/404`,
      ...docs.map((doc) => `${siteUrl}${doc.path}`),
      ...about.map((about) => `${siteUrl}${about.path}`),
    ]

    // 3. 生成XML（关键修改：添加换行和缩进）
    const xmlLines = [
      '<?xml version="1.0" encoding="UTF-8"?>',
      '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">',
    ]

    // 添加每个URL条目
    urls.forEach((url) => {
      xmlLines.push("  <url>")
      xmlLines.push(`    <loc>${url}</loc>`)
      xmlLines.push("  </url>")
    })

    // 闭合标签
    xmlLines.push("</urlset>")

    // 组合成最终字符串（用换行符连接）
    const sitemap = xmlLines.join("\n")

    // 4. 设置响应头
    setResponseHeader(event, "content-type", "application/xml")
    setResponseHeader(event, "Cache-Control", "public, max-age=3600")

    return sitemap
  } catch (error) {
    console.error("生成Sitemap失败:", error)

    // 失败时返回最小化版本（同样格式化）
    const fallbackSitemap = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>${siteUrl}</loc>
  </url>
</urlset>`

    setResponseHeader(event, "content-type", "application/xml")
    return fallbackSitemap
  }
})
```

### 2. 配置环境变量

在 `nuxt.config.ts` 中：

```typescript
export default defineNuxtConfig({
  runtimeConfig: {
    public: {
      siteUrl: process.env.SITE_URL,
    },
  },
})
```

在 `.env` 文件中：

```bash
NUXT_PUBLIC_SITE_URL=https://moongate.top
```

### 3. 配置 robots.txt

```typescript
// server/routes/robots.txt.ts
export default defineEventHandler((event) => {
  const siteUrl = useRuntimeConfig().public.siteUrl

  return `User-agent: *
Allow: /
Allow: /*.css$
Allow: /*.js$

Sitemap: ${siteUrl}/sitemap.xml`
})
```

## 生产环境部署要点

### GitHub Actions 配置

确保构建时传递环境变量：

```yaml
- name: Build
  run: |
    NUXT_PUBLIC_SITE_URL=https://moongate.top \
    pnpm run build
```

### PM2 配置文件

```javascript
// ecosystem.config.js
module.exports = {
  apps: [
    {
      name: "moongate",
      script: "./server/index.mjs",
      env: {
        NUXT_PUBLIC_SITE_URL: "https://moongate.top", // 关键！
        NODE_ENV: "production",
      },
    },
  ],
}
```

## 验证与测试

### 1. 本地验证

```bash
# 检查 XML 格式
curl http://localhost:3000/sitemap.xml | xmllint --format -

# 检查可访问性
curl -I http://localhost:3000/sitemap.xml
```

### 2. 在线工具验证

- [Google Sitemap 测试工具](https://search.google.com/search-console/sitemaps)
- [XML 验证器](https://www.xml-sitemaps.com/validate-xml-sitemap.html)

### 3. 提交到搜索引擎

1. Google Search Console → Sitemaps → 提交 URL
2. Bing Webmaster Tools → Sitemaps
3. 百度搜索资源平台 → 链接提交 → Sitemap

## 常见问题解决

### Q1: Sitemap 返回 404

- 检查文件路径：`server/routes/sitemap.xml.ts`
- 确认 Nuxt 服务器路由配置正确

### Q2: URL 不完整（缺少 https://）

```typescript
// 正确：完整的 URL
;`${siteUrl}/docs/${slug}`
// 错误：相对路径
`/docs/${slug}`
```

### Q3: 生产环境 siteUrl 为空

添加需要的环境变量

```yaml
- name: Build
  run: |
    NUXT_PUBLIC_SITE_URL=https://moongate.top \
    pnpm run build
```

> 这确保了 Nuxt 在构建时将 `public.siteUrl` 正确内嵌到客户端和服务端包中。

添加ecosystem.config.js配置脚本

```yaml
script: |
echo "🚀 启动 Node.js SSR 服务..."
cd /var/www/my-site

# 创建或更新 PM2 配置文件
cat > ecosystem.config.js << 'EOF'
module.exports = {
  apps: [{
    name: "moongate",
    script: "./server/index.mjs",
    instances: 1,
    exec_mode: "fork",
    env: {
      NODE_ENV: "production",
      NUXT_PUBLIC_SITE_URL: "https://moongate.top",
      PORT: 3000,
      HOST: "0.0.0.0"
    }
  }]
}
EOF

echo "📁 PM2 配置文件已生成"

# 使用配置文件管理应用
if pm2 describe moongate > /dev/null 2>&1; then
  echo "🔄 重启现有应用（使用最新配置）..."
  pm2 reload ecosystem.config.js --update-env
else
  echo "🚀 启动新应用..."
  pm2 start ecosystem.config.js
fi

# 保存 PM2 配置以便开机自启
pm2 save

echo "✅ 服务启动完成"
pm2 status moongate
```

## 最佳实践总结

1. **使用动态生成**：适合经常更新的博客
2. **包含所有重要页面**：首页、文档页、分类页、关于页
3. **使用绝对 URL**：始终包含 `https://`
4. **设置合理缓存**：`Cache-Control: public, max-age=3600`
5. **监控索引状态**：定期检查 Google Search Console
6. **保持更新**：内容更新后及时更新 `lastmod` 字段
7. **验证格式**：部署前使用 XML 验证工具检查

## 效果监测

部署 Sitemap 后，关注以下指标：

1. **索引覆盖率**（Google Search Console）
2. **爬行统计**：成功 vs 错误的 URL 数量
3. **搜索表现**：关键词排名和点击率变化
4. **收录速度**：新文档从发布到被收录的时间

通常配置正确的 Sitemap 能在 1-2 周内显著改善新内容的收录速度。

---

_本文基于 Nuxt 4 和实际部署经验编写，适用于使用 SSR 或静态生成的博客。根据你的具体需求调整数据源和 URL 结构。_
