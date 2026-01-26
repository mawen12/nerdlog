package domain

// HistoryRepository 定义历史记录仓储接口
// 用于持久化和加载历史记录
type HistoryRepository interface {
	// SaveItem 保存单条历史项
	SaveItem(item *HistoryItem) error

	// LoadAll 加载所有历史项
	LoadAll() ([]*HistoryItem, error)

	// Clear 清空所有历史
	Clear() error

	// DeleteItem 删除指定索引的项
	DeleteItem(index int) error
}

// HistoryStore 定义历史存储接口，由 application 层使用
// 隐藏持久化细节
type HistoryStore interface {
	// LoadCommandLineHistory 加载命令行历史
	LoadCommandLineHistory() (*CommandLineHistory, error)

	// SaveHistoryItem 保存历史项
	SaveHistoryItem(item *HistoryItem) error

	// LoadHistoryItems 加载所有历史项
	LoadHistoryItems() ([]*HistoryItem, error)

	// ClearHistory 清空历史
	ClearHistory() error
}
