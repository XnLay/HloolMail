package ratelimit

// 每个桶只占一个队列节点，查找和回收都不需要遍历整个索引。
type expiryQueue []*clientLimiter

func (q expiryQueue) Len() int { return len(q) }

func (q expiryQueue) Less(i, j int) bool {
	return q[i].expiresAt.Before(q[j].expiresAt)
}

func (q expiryQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index, q[j].index = i, j
}

func (q *expiryQueue) Push(value any) {
	entry := value.(*clientLimiter)
	entry.index = len(*q)
	*q = append(*q, entry)
}

func (q *expiryQueue) Pop() any {
	last := len(*q) - 1
	entry := (*q)[last]
	(*q)[last] = nil
	*q = (*q)[:last]
	entry.index = -1
	return entry
}
