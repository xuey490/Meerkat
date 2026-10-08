package util

// DefaultPageSize 是未传 pageSize 时的默认每页条数。
const DefaultPageSize = 20

// MaxPageSize 是单次请求的每页条数上限，避免单次查询拖垮 SQLite。
const MaxPageSize = 100

// NormalizePage 收敛分页参数 pageNum/pageSize。
//
// 参数 Parameters:
//   - page (int): 页码，小于 1 时按 1 处理。
//   - size (int): 每页条数，小于 1 时按默认值，超过上限时截断。
//
// 返回 Returns:
//   - normalizedPage (int): 有效页码。
//   - normalizedSize (int): 有效条数。
func NormalizePage(page, size int) (normalizedPage, normalizedSize int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}
	return page, size
}

// Offset 计算 SQL 分页偏移量。
//
// 参数 Parameters:
//   - page (int): 页码，从 1 开始。
//   - size (int): 每页条数。
//
// 返回 Returns:
//   - offset (int): 供 Offset 使用的偏移量，最小 0。
func Offset(page, size int) int {
	if page < 1 {
		page = 1
	}
	return (page - 1) * size
}
