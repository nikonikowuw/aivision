package repository

// normalizePage 分页参数归一：page<1→1；pageSize<1→20；>100→100。
// 各 ListPage 复用同一套分页上限约定，避免逐模块复制。
func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
