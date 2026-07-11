package domain

// ContentSetter 定义了设置正文内容的方法。
// 由 Doc、About 等需要存储 Markdown 正文转换结果的结构体实现。
type ContentSetter interface {
	SetContent(content string)
	SetSlug(slug string)
}
