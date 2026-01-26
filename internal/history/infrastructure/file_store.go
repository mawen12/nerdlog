package infrastructure

import (
	"os"
	"path/filepath"

	"github.com/dimonomid/nerdlog/internal/history/domain"
	"github.com/juju/errors"
)

// FileHistoryStore 基于文件的历史存储实现
type FileHistoryStore struct {
	filename  string
	marshaler *HistoryItemMarshaler
}

// NewFileHistoryStore 创建文件存储
func NewFileHistoryStore(filename string) *FileHistoryStore {
	return &FileHistoryStore{
		filename:  filename,
		marshaler: NewHistoryItemMarshaler(),
	}
}

// LoadCommandLineHistory 从文件加载命令行历史
func (s *FileHistoryStore) LoadCommandLineHistory() (*domain.CommandLineHistory, error) {
	history := domain.NewCommandLineHistory()

	if s.filename == "" {
		return history, nil
	}

	// 文件不存在时，返回空历史
	if _, err := os.Stat(s.filename); os.IsNotExist(err) {
		return history, nil
	}

	f, err := os.Open(s.filename)
	if err != nil {
		return nil, errors.Trace(err)
	}
	defer f.Close()

	items, err := s.marshaler.DecodeFromReaderAsItems(f)
	if err != nil {
		return nil, errors.Trace(err)
	}

	for _, item := range items {
		historyItem := domain.NewHistoryItemWithTime(item.Timestamp, item.Content)
		_ = history.Add(historyItem)
	}

	return history, nil
}

// SaveHistoryItem 保存单条历史项到文件
func (s *FileHistoryStore) SaveHistoryItem(item *domain.HistoryItem) error {
	if s.filename == "" {
		// 不持久化
		return nil
	}

	// 如果目录不存在，创建目录
	if _, err := os.Stat(s.filename); os.IsNotExist(err) {
		dir := filepath.Dir(s.filename)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errors.Trace(err)
		}
	}

	f, err := os.OpenFile(s.filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return errors.Trace(err)
	}
	defer f.Close()

	data := s.marshaler.Marshal(item.Timestamp(), item.Content())
	_, err = f.Write(data)
	return errors.Trace(err)
}

// LoadHistoryItems 加载所有历史项
func (s *FileHistoryStore) LoadHistoryItems() ([]*domain.HistoryItem, error) {
	history, err := s.LoadCommandLineHistory()
	if err != nil {
		return nil, err
	}
	return history.All(), nil
}

// ClearHistory 清空历史文件
func (s *FileHistoryStore) ClearHistory() error {
	if s.filename == "" {
		return nil
	}

	// 删除文件
	if err := os.Remove(s.filename); err != nil && !os.IsNotExist(err) {
		return errors.Trace(err)
	}

	return nil
}
