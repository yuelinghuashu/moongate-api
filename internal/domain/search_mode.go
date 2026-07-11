package domain

// 搜索模式
type SearchMode string

const (
	SearchModeAll         SearchMode = "all"         // 搜索所有字段（标题、描述）
	SearchModeTitle       SearchMode = "title"       // 搜索标题
	SearchModeDescription SearchMode = "description" // 搜索描述
)

// IsValid 检查 SearchMode 是否为允许的值（all, title, description）
func (m SearchMode) IsValid() bool {
	return m == SearchModeAll || m == SearchModeTitle || m == SearchModeDescription
}
