package interfaces

import (
	"github.com/dimonomid/nerdlog/internal/history/application"
	"github.com/dimonomid/nerdlog/internal/history/domain"
	"github.com/dimonomid/nerdlog/internal/history/infrastructure"
)

// HistoryFactory 历史记录的工厂，负责对象创建与装配
type HistoryFactory struct {
	store domain.HistoryStore
}

// NewHistoryFactory 创建工厂
func NewHistoryFactory(store domain.HistoryStore) *HistoryFactory {
	return &HistoryFactory{
		store: store,
	}
}

// NewHistoryFactoryWithFile 使用文件存储创建工厂
func NewHistoryFactoryWithFile(filename string) *HistoryFactory {
	fileStore := infrastructure.NewFileHistoryStore(filename)
	return &HistoryFactory{
		store: fileStore,
	}
}

// NewHistoryFactoryInMemory 创建基于内存的工厂
func NewHistoryFactoryInMemory() *HistoryFactory {
	memStore := infrastructure.NewInMemoryHistoryStore()
	return &HistoryFactory{
		store: memStore,
	}
}

// CreateCommandLineHistoryAdapter 创建命令行历史适配器
func (f *HistoryFactory) CreateCommandLineHistoryAdapter() (*CommandLineHistoryAdapter, error) {
	service := application.NewCommandLineHistoryService(f.store)
	if err := service.Load(); err != nil {
		return nil, err
	}
	return NewCommandLineHistoryAdapter(service), nil
}

// CreateBrowserLikeHistoryAdapter 创建浏览器式历史适配器
func (f *HistoryFactory) CreateBrowserLikeHistoryAdapter() *BrowserLikeHistoryAdapter {
	service := application.NewBrowserLikeHistoryService()
	return NewBrowserLikeHistoryAdapter(service)
}

// GetStore 获取底层存储（用于测试）
func (f *HistoryFactory) GetStore() domain.HistoryStore {
	return f.store
}
